# 51 — Нативный Telegram quiz poll

## Цель
Добавить второй UI-формат для квиза — нативный `sendPoll(type=quiz)`. Inline-вариант (шаг 50) остаётся; здесь делаем альтернативу, временно форсируя её для всех вопросов сессии, чтобы протестировать.

## Контекст
- Библиотека: `go-telegram/bot` (`models.SendPollParams`, `Type: "quiz"`, `CorrectOptionID`, `IsAnonymous: false`).
- Апдейт `Update.PollAnswer` приходит без `chat_id` — нужен лукап `pollID → (userID, chatID, wordID, sessionRef)`.
- Poll нельзя редактировать и нельзя прикрепить к нему inline-кнопку — фидбэк уходит **отдельным сообщением** после получения `poll_answer`.

## Что сделать
- В `internal/telegram/bot.go` (или соседнем файле, где регистрируются хендлеры) подключить обработчик `poll_answer`. В `go-telegram/bot` это обычно `b.RegisterHandler(bot.HandlerTypePollAnswer, …)` или дефолтный handler с разбором `update.PollAnswer`.
- Создать `internal/session/quiz_polls.go` (или соседний) — мапа `pollID → quizPollEntry { userID, chatID, wordID, correctIndex, createdAt }` с mutex и TTL (например 1 час).
- В хендлере квиза:
  - если `Item.UIMode == "poll"` — `bot.SendPoll(...)` с вопросом и опциями (длина текста ≤ 300, опций ≤ 100, обрезать `…`); сохранить запись в мапу.
  - после ответа в `poll_answer`: найти запись, вызвать `RecordCorrect`/`RecordWrong`, отправить feedback-сообщение (✅/❌ + пример) с кнопкой «Дальше» (`quiz:next`).
- Временно (для этого шага) форсируем `UIMode=poll` для всех вопросов сессии — чтобы пройти ручную проверку. На шаге 52 уберём оба форсажа.

## Definition of Done
- Ручной прогон раунда: все 10 вопросов приходят как нативные quiz-опросы, после клика подсвечивается правильный, отдельным сообщением приходит пример и кнопка «Дальше».
- `RecordCorrect`/`RecordWrong` срабатывают одинаково для обоих UI.
- Старые inline-вопросы продолжают работать, если форсаж снять (поведение шага 50 не сломано).
- `make build` зелёный, бот перезапущен через `pkill`.
- Один коммит `step 51: quiz-native-poll`.
