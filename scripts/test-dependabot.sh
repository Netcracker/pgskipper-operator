#!/bin/bash

# Скрипт для тестирования Dependabot конфигурации

echo "🔍 Проверка конфигурации Dependabot..."

# Проверка YAML синтаксиса
if command -v yamllint &> /dev/null; then
    echo "✅ Проверка YAML синтаксиса..."
    yamllint .github/dependabot.yml
else
    echo "⚠️  yamllint не установлен, пропускаем проверку YAML"
fi

# Проверка Go модулей
echo "📦 Проверка Go модулей..."
go mod tidy
go mod verify

# Проверка устаревших зависимостей
echo "🔍 Поиск устаревших зависимостей..."
go list -u -m all | grep -E "\[.*\]"

# Проверка Docker образов
echo "🐳 Проверка Docker образов..."
if [ -f "build/Dockerfile" ]; then
    echo "✅ Основной Dockerfile найден"
    grep "FROM" build/Dockerfile
fi

if [ -f "tests/Dockerfile" ]; then
    echo "✅ Тестовый Dockerfile найден"
    grep "FROM" tests/Dockerfile
fi

if [ -f "docker-transfer/Dockerfile" ]; then
    echo "✅ Dockerfile для передачи данных найден"
    grep "FROM" docker-transfer/Dockerfile
fi

# Проверка GitHub Actions
echo "⚡ Проверка GitHub Actions..."
if [ -d ".github/workflows" ]; then
    echo "✅ Workflows найдены"
    ls -la .github/workflows/
fi

echo "✅ Тестирование завершено!"
