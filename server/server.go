package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// Структура сообщения
type Message struct {
	Nick      string    `json:"nick"`
	Text      string    `json:"text"`
	Time      time.Time `json:"time"`
	Color     string    `json:"color"`
	Room      string    `json:"room"`
	IsPrivate bool      `json:"isPrivate"`
	Target    string    `json:"target"`
	ReplyTo   string    `json:"replyTo"`   // время сообщения на которое отвечаем
	ReplyText string    `json:"replyText"` // текст того сообщения
}

// Структура комнаты
type Room struct {
	Name           string
	Password       string // пустая строка = публичная комната
	CreatedBy      string
	CreatedAt      time.Time
	FailedAttempts map[string]*AttemptInfo // IP -> попытки
}

// Информация о попытках входа
type AttemptInfo struct {
	Count        int
	FirstAttempt time.Time
	BlockedUntil time.Time
}

// Клиент
type Client struct {
	conn         net.Conn
	nick         string
	color        string
	room         string
	lastSeen     time.Time
	firstMessage bool
	lastMsgTime  time.Time // ← добавить время последнего сообщения пользователя
}

// Сервер
type Server struct {
	clients     map[string]*Client
	history     []Message
	rooms       map[string]*Room // теперь не bool, а *Room
	mutex       sync.RWMutex
	historyFile string
	lastMessage *Message
	lastMsgTime time.Time
}

func NewServer() *Server {
	return &Server{
		clients:     make(map[string]*Client),
		history:     []Message{},
		rooms:       make(map[string]*Room),
		historyFile: "history.json",
		lastMessage: nil,
		lastMsgTime: time.Time{},
	}
}

func (s *Server) startCleaner() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			s.mutex.RLock()
			rooms := make([]string, 0, len(s.rooms))
			for name := range s.rooms {
				rooms = append(rooms, name)
			}
			s.mutex.RUnlock()

			for _, room := range rooms {
				s.cleanupEmptyRoom(room)
			}
		}
	}()
}

// Загрузка истории из файла
func (s *Server) loadHistory() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	data, err := os.ReadFile(s.historyFile)
	if err != nil {
		return
	}

	json.Unmarshal(data, &s.history)

	if len(s.history) > 0 {
		last := s.history[len(s.history)-1]
		s.lastMessage = &last
		s.lastMsgTime = last.Time
	}
}

// Сохранение истории в файл
func (s *Server) saveHistory() {
	s.mutex.RLock()
	data, _ := json.MarshalIndent(s.history, "", "  ")
	s.mutex.RUnlock()

	os.WriteFile(s.historyFile, data, 0644)
}

// Добавление сообщения в историю
func (s *Server) addMessage(msg Message) {
	if msg.IsPrivate {
		return // не сохраняем приватные сообщения
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.history = append(s.history, msg)
	if len(s.history) > 1000 {
		s.history = s.history[1:]
	}

	go s.saveHistory()
}

// Проверка, нужно ли показывать ник
func (s *Server) shouldShowNick(currentMsg Message) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if currentMsg.Nick == "✦" {
		return true
	}

	// Ищем ПРЕДЫДУЩЕЕ сообщение в комнате (НЕ последнее!)
	var prevMsgInRoom *Message
	for i := len(s.history) - 1; i >= 0; i-- {
		if s.history[i].Room == currentMsg.Room {
			// Если нашли текущее сообщение - пропускаем
			if s.history[i].Time.Equal(currentMsg.Time) && s.history[i].Nick == currentMsg.Nick {
				continue
			}
			prevMsgInRoom = &s.history[i]
			break
		}
	}

	if prevMsgInRoom == nil {
		log.Printf("ПЕРВОЕ СООБЩЕНИЕ В КОМНАТЕ: показываем ник")
		return true
	}

	log.Printf("СРАВНЕНИЕ: текущий='%s', предыдущий='%s'",
		currentMsg.Nick, prevMsgInRoom.Nick)

	if prevMsgInRoom.Nick != currentMsg.Nick {
		log.Printf("👉 ПРЕДЫДУЩЕЕ ОТ ДРУГОГО: показываем ник")
		return true
	}

	timeDiff := currentMsg.Time.Sub(prevMsgInRoom.Time)
	if timeDiff >= 5*time.Minute {
		log.Printf("👉 ПРОШЛО 5+ МИНУТ: показываем ник")
		return true
	}

	log.Printf("👉 ТОТ ЖЕ, МЕНЬШЕ 5 МИНУТ: НЕ показываем")
	return false
}

// Генерация цвета
func nickToColor(nick string) string {
	hash := 0
	for _, c := range nick {
		hash = (hash*31 + int(c)) & 0xFFFFFF
	}

	r := (hash >> 16) & 0xFF
	g := (hash >> 8) & 0xFF
	b := hash & 0xFF

	if r < 100 && g < 100 && b < 100 {
		r = (r + 150) % 256
		g = (g + 150) % 256
		b = (b + 150) % 256
	}

	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

// Отправка истории клиенту
func (s *Server) sendHistory(conn net.Conn, room string) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for i, msg := range s.history {
		if msg.Room != room {
			continue
		}

		// Проверяем, action ли это
		if strings.HasPrefix(msg.Text, "/me ") {
			action := strings.TrimPrefix(msg.Text, "/me ")
			line := fmt.Sprintf("[%s] \033[38;2;255;192;203m✦ %s %s\033[0m\n",
				msg.Time.Format("15:04"),
				msg.Nick,
				action)
			conn.Write([]byte(line))
			continue
		}

		// Обычное сообщение с группировкой
		showNick := true
		if i > 0 {
			prev := s.history[i-1]
			timeDiff := msg.Time.Sub(prev.Time)
			if prev.Nick == msg.Nick && timeDiff < 5*time.Minute && prev.Room == msg.Room {
				showNick = false
			}
		}

		if showNick {
			line := fmt.Sprintf("[%s] \033[38;2;%sm%s\033[0m: %s\n",
				msg.Time.Format("15:04"),
				msg.Color,
				msg.Nick,
				msg.Text)
			conn.Write([]byte(line))
		} else {
			line := fmt.Sprintf("[%s] \033[38;2;%sm   ╰─\033[0m %s\n",
				msg.Time.Format("15:04"),
				msg.Color,
				msg.Text)
			conn.Write([]byte(line))
		}
	}
}

// Рассылка в комнату
// Рассылка в комнату
func (s *Server) broadcastToRoom(msg Message, exclude string) {
	s.mutex.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.room == msg.Room {
			clients = append(clients, c)
		}
	}

	showNick := s.shouldShowNick(msg)
	s.mutex.RUnlock()

	var line string

	// ЕСЛИ ЭТО ОТВЕТ (/reply)
	if msg.ReplyTo != "" {
		// Многострочный ответ с контекстом
		line = fmt.Sprintf("[%s] \033[38;2;%sm%s\033[0m\n      ╰─ \033[38;2;255;255;0m▶ Ответ %s: %s\033[0m\n      ╰─ \033[38;2;%sm%s\033[0m\n",
			msg.Time.Format("15:04"),
			msg.Color,
			msg.Nick,
			msg.ReplyTo,   // ник кого ответили
			msg.ReplyText, // текст оригинального сообщения
			msg.Color,
			msg.Text) // свой текст
	} else {
		// ОБЫЧНОЕ СООБЩЕНИЕ (с группировкой или без)
		if showNick {
			line = fmt.Sprintf("[%s] \033[38;2;%sm%s\033[0m: %s\n",
				msg.Time.Format("15:04"),
				msg.Color,
				msg.Nick,
				msg.Text)
		} else {
			line = fmt.Sprintf("[%s] \033[38;2;%sm   ╰─\033[0m %s\n",
				msg.Time.Format("15:04"),
				msg.Color,
				msg.Text)
		}
	}

	// Отправляем всем в комнате
	for _, client := range clients {
		if client.nick != exclude {
			_, err := client.conn.Write([]byte(line))
			if err != nil {
				log.Printf("Ошибка отправки %s: %v", client.nick, err)
			}
		}
	}
}

// Обработка клиента
func (s *Server) handleClient(conn net.Conn) {
	defer conn.Close()

	conn.Write([]byte("прикольный ник) "))

	reader := bufio.NewReader(conn)
	nick, _ := reader.ReadString('\n')
	nick = strings.TrimSpace(nick)

	if nick == "" {
		return
	}

	s.mutex.Lock()
	if _, exists := s.clients[nick]; exists {
		s.mutex.Unlock()
		conn.Write([]byte("Такой ник уже занят. Отключаю...\n"))
		return
	}

	color := nickToColor(nick)
	client := &Client{
		conn:         conn,
		nick:         nick,
		color:        color,
		room:         "general",
		lastSeen:     time.Now(),
		firstMessage: true,
		lastMsgTime:  time.Time{}, // ← пустое время
	}
	s.clients[nick] = client

	// Проверяем/создаем комнату general
	if _, exists := s.rooms["general"]; !exists {
		s.rooms["general"] = &Room{
			Name:           "general",
			Password:       "",
			CreatedBy:      "system",
			CreatedAt:      time.Now(),
			FailedAttempts: make(map[string]*AttemptInfo),
		}
	}
	s.mutex.Unlock()

	conn.Write([]byte("\n--- История чата (комната general) ---\n"))
	s.sendHistory(conn, "general")
	conn.Write([]byte("--------------------------------------\n\n"))

	joinMsg := Message{
		Nick:  "✦",
		Text:  fmt.Sprintf("%s присоединился к чату", nick),
		Time:  time.Now(),
		Color: "255;255;0",
		Room:  "general",
	}
	s.addMessage(joinMsg)
	s.broadcastToRoom(joinMsg, "")

	for {
		text, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		switch {
		case text == "/quit":
			return

		case strings.HasPrefix(text, "/create "):
			// Формат: /create roomname password
			parts := strings.SplitN(strings.TrimPrefix(text, "/create "), " ", 2)
			roomName := strings.TrimSpace(parts[0])

			if roomName == "" {
				conn.Write([]byte("Укажите название комнаты\n"))
				continue
			}

			// Проверяем, нет ли уже такой комнаты
			s.mutex.Lock()
			if _, exists := s.rooms[roomName]; exists {
				s.mutex.Unlock()
				conn.Write([]byte("Комната уже существует\n"))
				continue
			}

			password := ""
			if len(parts) > 1 {
				password = strings.TrimSpace(parts[1])
			}

			// Создаем комнату
			room := &Room{
				Name:           roomName,
				Password:       password,
				CreatedBy:      nick,
				CreatedAt:      time.Now(),
				FailedAttempts: make(map[string]*AttemptInfo),
			}
			s.rooms[roomName] = room
			s.mutex.Unlock()

			// Переходим в новую комнату
			oldRoom := client.room
			client.room = roomName

			result := fmt.Sprintf("✅ Комната '%s' создана", roomName)
			if password != "" {
				result += " (защищена паролем)"
			}
			conn.Write([]byte(result + "\n"))

			// Уведомление
			createMsg := Message{
				Nick:  "✦",
				Text:  fmt.Sprintf("%s создал комнату %s", nick, roomName),
				Time:  time.Now(),
				Color: "255;255;0",
				Room:  roomName,
			}
			s.addMessage(createMsg)
			s.broadcastToRoom(createMsg, "")

			// Выходим из старой комнаты
			if oldRoom != roomName {
				leaveMsg := Message{
					Nick:  "✦",
					Text:  fmt.Sprintf("%s перешел в комнату %s", nick, roomName),
					Time:  time.Now(),
					Color: "255;255;0",
					Room:  oldRoom,
				}
				s.addMessage(leaveMsg)
				s.broadcastToRoom(leaveMsg, nick)
			}
			continue

		case text == "/help":
			helpText := `
		📋 ДОСТУПНЫЕ КОМАНДЫ:

		👥 ПОЛЬЗОВАТЕЛИ:
		/users                    - список пользователей в комнате
		/whois ник                - информация о пользователе

		💬 ЧАТ:
		/msg ник текст           - приватное сообщение
		/reply ник текст         - ответ на последнее сообщение пользователя
		/me действие             - action (например /me улыбнулся)

		🏠 КОМНАТЫ:
		/rooms                    - список всех комнат
		/join комната [пароль]    - войти в комнату
		/create комната [пароль]  - создать комнату
		/kick ник                 - кикнуть пользователя (только создатель)

		🔧 СИСТЕМА:
		/help                     - это сообщение
		/quit                      - выйти из чата

		Примеры:
		/msg vasya Привет!        - приватка
		/reply vasya И тебе привет! - ответ
		/create games 123         - создать комнату с паролем
		/join games 123           - войти в комнату
		`
			conn.Write([]byte(helpText))
			continue

		case strings.HasPrefix(text, "/me "):
			action := strings.TrimSpace(strings.TrimPrefix(text, "/me "))
			if action == "" {
				conn.Write([]byte("Укажите действие: /me улыбнулся\n"))
				continue
			}

			// Создаем action сообщение
			actionMsg := Message{
				Nick:  nick,
				Text:  "/me " + action, // сохраняем с префиксом для истории
				Time:  time.Now(),
				Color: "255;192;203", // нежно-розовый
				Room:  client.room,
			}

			// Форматируем специально для action
			actionLine := fmt.Sprintf("[%s] \033[38;2;255;192;203m✦ %s %s\033[0m\n",
				actionMsg.Time.Format("15:04"),
				nick,
				action)

			// Рассылаем всем в комнате
			s.mutex.RLock()
			for _, c := range s.clients {
				if c.room == client.room && c.nick != nick {
					c.conn.Write([]byte(actionLine))
				}
			}
			s.mutex.RUnlock()

			// Отправитель тоже видит
			conn.Write([]byte(actionLine))

			// Сохраняем в историю
			s.addMessage(actionMsg)

			client.lastSeen = time.Now()
			client.lastMsgTime = time.Now()
			continue

		case strings.HasPrefix(text, "/reply "):
			// Формат: /reply ник текст
			parts := strings.SplitN(strings.TrimPrefix(text, "/reply "), " ", 2)
			if len(parts) < 2 {
				conn.Write([]byte("Использование: /reply ник текст\n"))
				continue
			}

			targetNick := strings.TrimSpace(parts[0])
			replyText := strings.TrimSpace(parts[1])

			if targetNick == "" || replyText == "" {
				conn.Write([]byte("Использование: /reply ник текст\n"))
				continue
			}

			// Ищем ПОСЛЕДНЕЕ сообщение этого пользователя в текущей комнате
			s.mutex.RLock()
			var originalMsg *Message
			for i := len(s.history) - 1; i >= 0; i-- {
				if s.history[i].Room == client.room && s.history[i].Nick == targetNick {
					originalMsg = &s.history[i]
					break
				}
			}
			s.mutex.RUnlock()

			if originalMsg == nil {
				conn.Write([]byte(fmt.Sprintf("Нет сообщений от %s в этой комнате\n", targetNick)))
				continue
			}

			// Создаем сообщение-ответ
			replyMsg := Message{
				Nick:      nick,
				Text:      replyText,
				Time:      time.Now(),
				Color:     color,
				Room:      client.room,
				ReplyTo:   targetNick,       // теперь ник
				ReplyText: originalMsg.Text, // текст оригинального сообщения
			}

			s.addMessage(replyMsg)
			s.broadcastToRoom(replyMsg, "")

			client.lastSeen = time.Now()
			client.lastMsgTime = time.Now()
			continue

		case strings.HasPrefix(text, "/msg "):
			parts := strings.SplitN(strings.TrimPrefix(text, "/msg "), " ", 2)
			if len(parts) < 2 {
				conn.Write([]byte("Использование: /msg ник сообщение\n"))
				continue
			}

			targetNick := strings.TrimSpace(parts[0])
			msgText := strings.TrimSpace(parts[1])

			if targetNick == "" || msgText == "" {
				conn.Write([]byte("Использование: /msg ник сообщение\n"))
				continue
			}

			s.mutex.RLock()
			targetClient, exists := s.clients[targetNick]
			s.mutex.RUnlock()

			if !exists {
				conn.Write([]byte(fmt.Sprintf("Пользователь '%s' не найден\n", targetNick)))
				continue
			}

			// Отправляем получателю
			targetMsg := fmt.Sprintf("\033[38;2;%sm[Приват от %s]\033[0m: %s\n",
				color, nick, msgText)
			targetClient.conn.Write([]byte(targetMsg))

			// Подтверждение отправителю
			confirmMsg := fmt.Sprintf("\033[38;2;%sm[Приват для %s]\033[0m: %s\n",
				targetClient.color, targetNick, msgText)
			conn.Write([]byte(confirmMsg))

			// Лог
			log.Printf("🔒 Приват: %s -> %s: %s", nick, targetNick, msgText)
			continue

		case text == "/users":
			s.mutex.RLock()
			users := make([]string, 0, len(s.clients))
			for _, c := range s.clients {
				if c.room == client.room {
					users = append(users, c.nick)
				}
			}
			s.mutex.RUnlock()
			conn.Write([]byte("В комнате: " + strings.Join(users, ", ") + "\n"))
			continue

		case strings.HasPrefix(text, "/join "):
			// Формат: /join roomname password
			parts := strings.SplitN(strings.TrimPrefix(text, "/join "), " ", 2)
			roomName := strings.TrimSpace(parts[0])

			if roomName == "" {
				conn.Write([]byte("Укажите название комнаты\n"))
				continue
			}

			// Проверяем существование комнаты
			s.mutex.RLock()
			room, exists := s.rooms[roomName]
			s.mutex.RUnlock()

			if !exists {
				conn.Write([]byte("Комната не существует. Создайте её через /create\n"))
				continue
			}

			// Получаем пароль если есть
			password := ""
			if len(parts) > 1 {
				password = parts[1]
			}

			// Проверяем пароль (с защитой)
			clientIP := getClientIP(conn)
			if !s.checkRoomPassword(roomName, password, clientIP) {
				if room.Password != "" {
					conn.Write([]byte("❌ Неверный пароль или слишком много попыток. Подождите 5 минут.\n"))
				} else {
					conn.Write([]byte("❌ Не удалось войти в комнату\n"))
				}
				continue
			}

			// Успешный вход
			oldRoom := client.room
			client.room = roomName

			conn.Write([]byte(fmt.Sprintf("✅ Вошли в комнату: %s\n", roomName)))
			conn.Write([]byte(fmt.Sprintf("\n--- История чата (комната %s) ---\n", roomName)))
			s.sendHistory(conn, roomName)
			conn.Write([]byte("------------------------------------\n\n"))

			joinMsg := Message{
				Nick:  "✦",
				Text:  fmt.Sprintf("%s присоединился к комнате %s", nick, roomName),
				Time:  time.Now(),
				Color: "255;255;0",
				Room:  roomName,
			}
			s.addMessage(joinMsg)
			s.broadcastToRoom(joinMsg, nick)

			// Уведомление в старой комнате
			if oldRoom != roomName {
				// Уведомление в старой комнате
				leaveMsg := Message{
					Nick:  "✦",
					Text:  fmt.Sprintf("%s перешел в комнату %s", nick, roomName),
					Time:  time.Now(),
					Color: "255;255;0",
					Room:  oldRoom,
				}
				s.addMessage(leaveMsg)
				s.broadcastToRoom(leaveMsg, nick)

				// ** Проверяем старую комнату на пустоту **
				go s.cleanupEmptyRoom(oldRoom)
			}
			continue

		case text == "/rooms":
			s.mutex.RLock()
			roomsList := make([]string, 0, len(s.rooms))
			for name, room := range s.rooms {
				usersCount := 0
				for _, client := range s.clients {
					if client.room == name {
						usersCount++
					}
				}

				if room.Password == "" {
					roomsList = append(roomsList, fmt.Sprintf("%s (публичная, %d чел)", name, usersCount))
				} else {
					roomsList = append(roomsList, fmt.Sprintf("%s 🔒 (%d чел)", name, usersCount))
				}
			}
			s.mutex.RUnlock()

			if len(roomsList) == 0 {
				conn.Write([]byte("Нет комнат. Создайте через /create\n"))
			} else {
				conn.Write([]byte("Комнаты:\n"))
				for _, r := range roomsList {
					conn.Write([]byte("  " + r + "\n"))
				}
			}
			continue

		default:
			msg := Message{
				Nick:  nick,
				Text:  text,
				Time:  time.Now(),
				Color: color,
				Room:  client.room,
			}
			s.addMessage(msg)
			s.broadcastToRoom(msg, "")
			client.lastSeen = time.Now()    // ← это уже есть
			client.lastMsgTime = time.Now() // ← добавил правильно

		}
		if text == "/quit" {
			break
		}
	}

	s.mutex.Lock()
	delete(s.clients, nick)
	oldRoom := client.room // сохраняем комнату перед удалением клиента
	s.mutex.Unlock()

	// Сообщаем об уходе
	leaveMsg := Message{
		Nick:  "✦",
		Text:  fmt.Sprintf("%s покинул чат", nick),
		Time:  time.Now(),
		Color: "255;255;0",
		Room:  oldRoom,
	}
	s.addMessage(leaveMsg)
	s.broadcastToRoom(leaveMsg, "")

	// ** ВАЖНО: проверяем, не опустела ли комната **
	go s.cleanupEmptyRoom(oldRoom)
}

// TCP режим
func (s *Server) runTCP(port string) error {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("TCP сервер запущен на порту %s", port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Ошибка подключения: %v", err)
			continue
		}

		go s.handleClient(conn)
	}
}

// Tailscale режим
func (s *Server) runTailscale(hostname, port string) error {
	srv := &tsnet.Server{
		Hostname: hostname,
	}
	defer srv.Close()

	ln, err := srv.Listen("tcp", ":"+port)
	if err != nil {
		return err
	}
	defer ln.Close()

	log.Printf("Tailscale сервер запущен как %s.ts.net:%s", hostname, port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Ошибка подключения: %v", err)
			continue
		}

		go s.handleClient(conn)
	}
}

func main() {
	server := NewServer()
	server.loadHistory()
	go server.startCleaner()

	if len(os.Args) < 2 {
		fmt.Println("Использование:")
		fmt.Println("  TCP режим:     go run server.go tcp [порт]")
		fmt.Println("  Tailscale режим: go run server.go ts [хостнейм] [порт]")
		return
	}

	mode := os.Args[1]

	switch mode {
	case "tcp":
		port := "2323"
		if len(os.Args) > 2 {
			port = os.Args[2]
		}
		log.Fatal(server.runTCP(port))

	case "ts":
		if len(os.Args) < 3 {
			log.Fatal("Укажите хостнейм для Tailscale")
		}
		hostname := os.Args[2]
		port := "2323"
		if len(os.Args) > 3 {
			port = os.Args[3]
		}
		log.Fatal(server.runTailscale(hostname, port))

	default:
		fmt.Println("Неизвестный режим. Используйте 'tcp' или 'ts'")
	}
}

// Проверка пароля с защитой от брутфорса
func (s *Server) checkRoomPassword(roomName, password, clientIP string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	room, exists := s.rooms[roomName]
	if !exists {
		return false
	}

	// Публичная комната
	if room.Password == "" {
		return true
	}

	// Инициализация защиты
	if room.FailedAttempts == nil {
		room.FailedAttempts = make(map[string]*AttemptInfo)
	}

	now := time.Now()
	info := room.FailedAttempts[clientIP]

	// Проверка блокировки
	if info != nil && now.Before(info.BlockedUntil) {
		return false
	}

	// Проверка пароля
	if password == room.Password {
		// Успешный вход — сбрасываем попытки
		delete(room.FailedAttempts, clientIP)
		return true
	}

	// Неудачная попытка
	if info == nil {
		info = &AttemptInfo{
			Count:        1,
			FirstAttempt: now,
		}
		room.FailedAttempts[clientIP] = info
	} else {
		info.Count++

		// Блокировка после 5 попыток
		if info.Count >= 5 {
			info.BlockedUntil = now.Add(5 * time.Minute)
			log.Printf("⚠️ IP %s заблокирован на 5 минут (комната %s)", clientIP, roomName)
		}
	}

	return false
}

// Получение IP клиента
func getClientIP(conn net.Conn) string {
	addr := conn.RemoteAddr().String()
	// Обрезаем порт
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		addr = addr[:idx]
	}
	return addr
}

// Подсчет пользователей в комнате
func (s *Server) countUsersInRoom(roomName string) int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	count := 0
	for _, client := range s.clients {
		if client.room == roomName {
			count++
		}
	}
	return count
}

// Удаление пустой комнаты (если там 0 пользователей и это не general)
func (s *Server) cleanupEmptyRoom(roomName string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Не удаляем general
	if roomName == "general" {
		return
	}

	// Проверяем, есть ли еще кто-то в комнате
	usersCount := 0
	for _, client := range s.clients {
		if client.room == roomName {
			usersCount++
		}
	}

	// Если пользователей нет - удаляем комнату
	if usersCount == 0 {
		if _, exists := s.rooms[roomName]; exists { // ← исправлено!
			log.Printf("🧹 Комната '%s' удалена (пуста)", roomName)
			delete(s.rooms, roomName)
		}
	}
}
