#!/bin/sh

# launch.sh - Полная установка и запуск мессенджера для iSH

echo "Запуск установки мессенджера для iSH"

# 1. Обновление пакетов
echo "Обновление пакетов..."
apk update > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "Пакеты обновлены"
else
    echo "Ошибка обновления пакетов"
    exit 1
fi

# 2. Проверка/установка Go
echo "Проверка Go..."
if ! command -v go &> /dev/null; then
    echo "Установка Go..."
    apk add go > /dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "Go установлен: $(go version)"
    else
        echo "Ошибка установки Go"
        exit 1
    fi
else
    echo "Go уже установлен: $(go version)"
fi

# 3. Проверка/установка Git
echo "Проверка Git..."
if ! command -v git &> /dev/null; then
    echo "Установка Git..."
    apk add git > /dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "Git установлен: $(git --version)"
    else
        echo "Ошибка установки Git"
        exit 1
    fi
else
    echo "Git уже установлен: $(git --version)"
fi

# 4. Клонирование репозитория (если нет)
if [ ! -d "vibe_mess" ]; then
    echo "Клонирование репозитория..."
    git clone https://github.com/SkySweet1/vibe_mess.git
    if [ $? -eq 0 ]; then
        echo "Репозиторий склонирован"
    else
        echo "Ошибка клонирования"
        exit 1
    fi
else
    echo "Репозиторий уже существует"
    echo "Обновление репозитория..."
    cd vibe_mess && git pull && cd ..
fi

# 5. Переход в папку клиента
cd vibe_mess/client || { echo "Папка client не найдена"; exit 1; }

# 6. Сборка клиента
echo "Сборка клиента..."
go build -o messenger client.go
if [ $? -eq 0 ]; then
    echo "Клиент собран: $(ls -la messenger | awk '{print $5}') байт"
else
    echo "Ошибка сборки"
    exit 1
fi

# 7. Информация о Tailscale
echo "ready"