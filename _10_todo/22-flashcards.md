# Шаг 22. Учебные сессии (flashcards) (6–8h)

## Цель
Изучение слов в статусе `learning`. После 3 правильных подряд →
`mastered`. Ошибка обнуляет streak.

## Действия
1. FSM сессии в `internal/session/study.go`. Старт через
   `/study` или кнопку.
2. Выборка ~10 слов: `user_word_status WHERE status='learning'
   ORDER BY updated_at ASC LIMIT 10` (давно не повторённые
   приоритетнее).
3. Для каждого слова показ карточки: `surface_form`, `transcription
   _ipa`, пример. Кнопки «Помню» / «Не помню» / «Завершить сессию».
4. Логика:
   - «Помню» → `correct_streak++`, `correct_total++`, `updated_at=
     now()`. Если `correct_streak >= 3` — статус `mastered`.
   - «Не помню» → `correct_streak=0`, `wrong_total++`.
5. После последней карточки или «Завершить» — итоговая сводка
   (правильно/неправильно/`mastered` за сессию).

## DoD
- 3 правильных подряд → `status=mastered`.
- Ошибка после 2 правильных → счётчик начинается заново.
- Сессия корректно завершается и показывает сводку.
- `make build` + `make test` зелёные (тест на FSM-логику).

## Зависимости
Шаг 21.
