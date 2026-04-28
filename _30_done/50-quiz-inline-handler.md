# 50 — Хендлер квиза (inline-кнопки)

## Цель
Заменить старый карточечный `/study` на квиз с inline-кнопками. Команда `/study` сохраняется, но открывает теперь квиз. Нативный poll-вариант добавим в шаге 51.

## Контекст
Сборка сессии и FSM готовы (шаги 48, 49). На этом шаге временно форсируем `UIMode=inline` для всех элементов сессии — чтобы не тянуть весь poll-стек одновременно. После шага 51 это снимется.

## Что сделать
- Переписать `internal/telegram/handlers/study.go`:
  - `/study`: создаёт `QuizSession`, при пустом словаре отвечает локализованным `quiz.empty`.
  - Рендер вопроса:
    - заголовок `Карточка N/10`;
    - для `fwd`: лемма + IPA;
    - для `bwd`: перевод (без IPA);
    - 4 кнопки с вариантами `quiz:ans:<wordID>:<idx>`; кнопка «Завершить» `quiz:end`.
  - Callback:
    - проверка совпадения курсора (защита от устаревших нажатий);
    - вызов `RecordCorrect(ctx, db, userID, wordID, threshold=3)` или `RecordWrong`;
    - редактирование сообщения: ✅/❌ + подсветка правильного варианта (например жирный) + пример (`ExampleForeign` / `ExampleNative`);
    - кнопка «Дальше» `quiz:next` или «Закрыть» если раунд закончен.
  - Сводка раунда: `Правильно X/10`, список выученных в этом раунде; кнопки «Ещё 10» (`quiz:again`) и «Закрыть» (`quiz:close`).
- i18n (`en.yaml`/`es.yaml`/`ru.yaml`): добавить ключи
  `quiz.empty`, `quiz.card.header`, `quiz.feedback.correct`, `quiz.feedback.wrong`,
  `quiz.feedback.example`, `quiz.btn.next`, `quiz.btn.end`,
  `quiz.summary.header`, `quiz.summary.score`, `quiz.summary.mastered_header`,
  `quiz.btn.again`, `quiz.btn.close`.
- Удалить мёртвые i18n-ключи и код самооценки (`study.btn.hit`/`study.btn.miss`/`study.card.*` и связанная FSM `internal/session/study.go`, если больше не используется).
- На этом шаге в `BuildQuizSession` принудительно ставим `UIMode=inline` (через флаг билдера или константу) — на шаге 51 уберём.

## Definition of Done
- Полный сценарий вручную: добавить ≥10 слов в `learning`, запустить `/study`, пройти 10 вопросов, увидеть сводку.
- На правильном ответе у слова растёт `correct_streak`; после 3 — слово в `mastered` и больше не появляется.
- `make build` зелёный. После сборки: `pkill -f bin/tg-linguine` и проверка нового PID через ~70с.
- Один коммит `step 50: quiz-inline-handler`.

## Ревью
Шаг 50 кратен 5 — после закрытия задачи провести code review проектных изменений и при необходимости создать рефактор-задачи `49.5-…`, `49.6-…` (формула из CLAUDE.md).
