#!/bin/bash

detect_os() {
    case "$(uname -s)" in
        Linux*)
            if [ -d "/data/data/com.termux" ]; then
                echo "termux"
            else
                echo "linux"
            fi
            ;;
        Darwin*)    echo "macos" ;;
        CYGWIN*|MINGW*|MSYS*) echo "windows" ;;
        *)          echo "unknown" ;;
    esac
}

OS=$(detect_os)
echo "Определена платформа: $OS"

case $OS in
    termux)
        pkg update -y
        pkg install -y golang git
        ;;
    macos)
        if ! command -v brew &> /dev/null; then
            /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
        fi
        brew install go git
        ;;
    windows)
        echo "Для Windows используй launch-windows.bat"
        exit 0
        ;;
    linux)
        echo "Установи Go и git вручную:"
        echo "  sudo apt update && sudo apt install golang git"
        ;;
esac

# Клонирование и сборка
[ ! -d "vibe_mess" ] && git clone https://github.com/SkySweet1/vibe_mess.git
cd vibe_mess/client && go build -o messenger client.go

echo "Готово! Запусти: ./messenger IP:2323"