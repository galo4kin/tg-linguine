# 49 — FSM сессии квиза (in-memory)

## Цель
In-memory машина состояний для одного раунда квиза из 10 вопросов. Аналог `internal/session/study.go`, но для multiple choice.

## Контекст
План: квиз-режим с двумя направлениями (foreign→native, native→foreign) и двумя UI (inline / нативный poll), которые случайно чередуются. Логика мастери прежняя — `correct_streak >= 3` через `dictionary.UserWordStatusRepository.RecordCorrect/RecordWrong`.

## Что сделать
- Создать `internal/session/quiz.go`:
  ```go
  type QuizDirection string  // "fwd" foreign→native, "bwd" native→foreign
  type QuizUIMode string     // "inline", "poll"

  type QuizItem struct {
      WordID         int64
      Lemma          string  // foreign
      IPA            string
      Translation    string  // native
      ExampleForeign string
      ExampleNative  string
      Direction      QuizDirection
      UIMode         QuizUIMode
      Options        []string  // 4 варианта, перемешаны
      CorrectIndex   int
  }

  type QuizSession struct { /* UserID, Items, Cursor, Correct, Wrong, Mastered []int64, StartedAt, mu */ }
  ```
- Регистр `Quiz` с TTL по аналогии с `Study` (30 минут).
- Билдер `BuildQuizSession(ctx, deps, userID, langCode, size=10)`:
  - `LearningQueue` для исходных слов;
  - на каждое слово случайно выбирает `Direction` и `UIMode` (50/50 каждое);
  - тянет 3 дистрактора через `SampleDistractors` (шаг 48);
  - формирует `Options` (1 правильный + 3 дистрактора), перемешивает, запоминает `CorrectIndex`.
- Методы сессии: `Current()`, `Answer(idx int) (correct bool, mastered bool)`, `Advance()`, `Done() bool`, `Summary()`.
- Если `LearningQueue` вернула меньше 10 слов — собрать из того, что есть; если 0 — билдер возвращает `ErrEmptyQueue`.

## Definition of Done
- Тесты на: сборку сессии (правильный набор и перемешивание опций), переход курсора, корректный учёт правильных/неправильных, флаг mastered после 3 правильных подряд.
- `make build` / `make test` зелёные.
- Один коммит `step 49: quiz-session-fsm`.
