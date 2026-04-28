# 48 — Подбор дистракторов для квиза

## Цель
Подготовить фундамент игрового режима: метод репозитория, который для целевого слова возвращает 3 случайных «неправильных» варианта ответа.

## Контекст
Сейчас `/study` показывает карточку с самооценкой «Помню/Не помню». В шагах 49–55 он будет заменён на квиз с 4 вариантами ответа в двух направлениях (foreign↔native). И inline-режим, и нативный Telegram quiz poll должны брать дистракторы из одного места.

Где брать переводы — посмотреть существующие чтения в `internal/dictionary/repo.go` и в обработчике карточки (`internal/telegram/handlers/study.go`, `renderStudyCard`). Использовать тот же источник, что и текущий рендер.

## Что сделать
- В `internal/dictionary/repo.go` (или соседнем файле в пакете) добавить:
  ```go
  type DistractorDirection string
  const (
      DistractorForeignToNative DistractorDirection = "fwd" // ответы — переводы (родной)
      DistractorNativeToForeign DistractorDirection = "bwd" // ответы — леммы (foreign)
  )

  func (r *UserWordStatusRepository) SampleDistractors(
      ctx context.Context, db DB,
      userID int64, languageCode string,
      excludeWordID int64, correctAnswer string,
      direction DistractorDirection, n int,
  ) ([]string, error)
  ```
- Гарантии:
  - результат уникален; не содержит `correctAnswer` (case-insensitive trim);
  - в первую очередь берёт значения из словаря пользователя (`user_word_status` join `dictionary_words`);
  - если в пользовательском словаре наберётся меньше `n` — добивает из общего пула слов того же `language_code`;
  - возвращает ровно `n` или ошибку, если в БД настолько мало слов, что заполнить нечем (для очень новых пользователей это нормально — обработать вызывающим).

## Definition of Done
- Метод реализован, вызывается рандомизация (`ORDER BY RANDOM()` в SQLite или через рандомное смещение).
- Юнит-тест(ы): корректное исключение целевого слова, заполнение из общего пула при дефиците, оба направления.
- `make build` и `make test` зелёные.
- Один коммит `step 48: quiz-distractor-selection`.
