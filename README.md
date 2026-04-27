# tg-linguine

Telegram-бот для изучения иностранных языков. Проект на **TypeScript**, бот построен на [grammY](https://grammy.dev/).

## Требования

- Node.js 20+
- Токен бота от [@BotFather](https://t.me/BotFather)

## Быстрый старт

```bash
npm install
cp .env.example .env
# Укажите BOT_TOKEN в .env
npm run dev
```

Для production-сборки:

```bash
npm run build
npm start
```

## Переменные окружения

См. [.env.example](.env.example). Минимально нужен `BOT_TOKEN`.

## Скрипты

| Команда | Описание |
|--------|----------|
| `npm run dev` | Запуск с hot-reload (tsx) |
| `npm run build` | Компиляция TypeScript в `dist/` |
| `npm start` | Запуск скомпилированного бота |

## Лицензия

Проект в разработке; лицензия пока не указана.
