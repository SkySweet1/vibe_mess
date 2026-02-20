#!/bin/bash

echo "Установка мессенджера для macOS"

# Проверка Homebrew
if ! command -v brew &> /dev/null; then
    echo "Установка Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi

# Установка Go
if ! command -v go &> /dev/null; then
    echo "Установка Go..."
    brew install go
else
    echo "Go уже установлен: $(go version)"
fi

# Установка Git
if ! command -v git &> /dev/null; then
    echo "Установка Git..."
    brew install git
else
    echo "Git уже установлен: $(git --version)"
fi

# Клонирование
if [ ! -d "vibe_mess" ]; then
    echo "Клонирование репозитория..."
    git clone https://github.com/SkySweet1/vibe_mess.git
else
    echo "Репозиторий уже есть, обновляем..."
    cd vibe_mess && git pull && cd ..
fi

# Сборка
cd vibe_mess/client
echo "Сборка клиента..."
go build -o messenger client.go

echo ""
echo "Запуск: cd vibe_mess/client && ./messenger IP:2323"