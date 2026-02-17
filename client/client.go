package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
)

func clearScreen() {
	fmt.Print("\033[3J")
	fmt.Print("\033[2J")
	fmt.Print("\033[H")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Использование:")
		fmt.Println("  go run client.go <адрес:порт>")
		fmt.Println("Пример:")
		fmt.Println("  go run client.go localhost:2323")
		fmt.Println("  go run client.go my-chat.ts.net:2323")
		return
	}

	address := os.Args[1]

	conn, err := net.Dial("tcp", address)
	if err != nil {
		color.Red("Ошибка подключения: %v", err)
		return
	}
	defer conn.Close()

	color.Green("✅ Подключено к %s", address)
	color.Cyan("📝 Команды: /users, /join комната [пароль], /create комната [пароль], /rooms, /msg ник текст, /reply ник текст, /help, /quit")

	done := make(chan bool)

	// Чтение от сервера
	go func() {
		reader := bufio.NewReader(conn)
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				color.Red("❌ Соединение с сервером потеряно")
				done <- true
				return
			}
			fmt.Print(msg)
		}
	}()

	// Чтение ввода пользователя
	go func() {
		stdinReader := bufio.NewReader(os.Stdin)

		for {
			text, _ := stdinReader.ReadString('\n')
			text = strings.TrimSpace(text)

			if text == "" {
				continue
			}

			// ← ИСПРАВЛЕНО: добавляем "_," чтобы принять два возвращаемых значения
			_, err := conn.Write([]byte(text + "\n"))
			if err != nil {
				done <- true
				return
			}

			if text == "/quit" {
				done <- true
				return
			}
		}
	}()

	<-done

	// Небольшая задержка чтобы последние сообщения ушли
	time.Sleep(100 * time.Millisecond)

	clearScreen()
	color.Yellow("👋 Чат завершен")
	time.Sleep(1 * time.Second)
}
