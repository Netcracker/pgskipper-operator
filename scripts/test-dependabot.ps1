# Скрипт для тестирования Dependabot конфигурации (PowerShell)

Write-Host "🔍 Проверка конфигурации Dependabot..." -ForegroundColor Green

# Проверка существования файла конфигурации
if (Test-Path ".github/dependabot.yml") {
    Write-Host "✅ Файл конфигурации Dependabot найден" -ForegroundColor Green
    Get-Content ".github/dependabot.yml"
} else {
    Write-Host "❌ Файл конфигурации Dependabot не найден" -ForegroundColor Red
    exit 1
}

# Проверка Go модулей
Write-Host "📦 Проверка Go модулей..." -ForegroundColor Green
try {
    go mod tidy
    go mod verify
    Write-Host "✅ Go модули проверены" -ForegroundColor Green
} catch {
    Write-Host "❌ Ошибка при проверке Go модулей: $_" -ForegroundColor Red
}

# Проверка устаревших зависимостей
Write-Host "🔍 Поиск устаревших зависимостей..." -ForegroundColor Green
try {
    $outdated = go list -u -m all | Select-String "\[.*\]"
    if ($outdated) {
        Write-Host "📋 Найдены устаревшие зависимости:" -ForegroundColor Yellow
        $outdated
    } else {
        Write-Host "✅ Все зависимости актуальны" -ForegroundColor Green
    }
} catch {
    Write-Host "⚠️  Не удалось проверить устаревшие зависимости" -ForegroundColor Yellow
}

# Проверка Docker образов
Write-Host "🐳 Проверка Docker образов..." -ForegroundColor Green

$dockerfiles = @(
    "build/Dockerfile",
    "tests/Dockerfile", 
    "docker-transfer/Dockerfile"
)

foreach ($dockerfile in $dockerfiles) {
    if (Test-Path $dockerfile) {
        Write-Host "✅ $dockerfile найден" -ForegroundColor Green
        $fromLines = Get-Content $dockerfile | Select-String "FROM"
        $fromLines
    } else {
        Write-Host "⚠️  $dockerfile не найден" -ForegroundColor Yellow
    }
}

# Проверка GitHub Actions
Write-Host "⚡ Проверка GitHub Actions..." -ForegroundColor Green
if (Test-Path ".github/workflows") {
    Write-Host "✅ Workflows найдены:" -ForegroundColor Green
    Get-ChildItem ".github/workflows" | ForEach-Object {
        Write-Host "  - $($_.Name)" -ForegroundColor Cyan
    }
} else {
    Write-Host "⚠️  Workflows не найдены" -ForegroundColor Yellow
}

Write-Host "✅ Тестирование завершено!" -ForegroundColor Green
