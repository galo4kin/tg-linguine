# Releases

## 30.5 — refactor-stub-llm-vs-mock-provider
Тесты `internal/articles/` переведены с локального `stubLLM` на
канонический `internal/llm/mock.Provider`. Поля `calls` /
`lastRequest` / `adaptCalls` / `lastAdaptRequest` заменены на
`AnalyzeCalls` / `AdaptCalls`, тип `stubLLM` удалён —
fixture-driven мок теперь единственная точка правды для тестов
LLM-провайдера. `make build` и `make test` зелёные.

## 32 — admin-commands
В боте появились три служебные команды: `/stats` (всего пользователей,
активных за 24ч/7д, статей всего и за сутки, слов в словаре), `/whoami`
(роль + Telegram-id) и `/shutdown` (graceful exit, watchdog поднимает
новый pid). `/stats` и `/shutdown` для не-админов молчат — наличие
админ-режима наружу не светится. Добавлена миграция
`0007_users_last_seen` (`last_seen_at` + индекс) и middleware
`touchLastSeenMiddleware`, который трогает поле на каждом апдейте от
юзера. Репозитории `users`, `articles`, `dictionary` обзавелись
агрегатными счётчиками; i18n-ключи `admin.*` синхронно появились в
en/ru/es. Покрыто тестом `users.Stats` (бэкдейтинг + Touch).

## 31 — admin-role
Введён фундамент для админ-функций: `ADMIN_USER_ID` в
`internal/config/config.go` (опциональный, default `0` = админ
выключен), `telegram.IsAdmin(cfg, userID)` как единственный гейт
(всегда `false` при `AdminUserID == 0`, защищено от подбора нулём).
На старте `cmd/bot/main.go` пишет `admin configured user_id=…` или
`admin disabled`. README пополнился разделом «Admin» с инструкцией
по `@userinfobot`, `.env.example` — закомментированной строкой.

## 30 — readme-sync
README приведён к фактическому коду: добавлены недостающие env
(`GROQ_MODEL`, `RATE_LIMIT_PER_HOUR`, `MAX_TOKENS_PER_ARTICLE`,
`LOG_*`), таблица env совпадает с `internal/config/config.go` и
`.env.example` 1:1. Появились разделы «Миграции» (auto on start через
`embed` + `golang-migrate`), «Команды бота» (полная таблица:
`/start`, `/setkey`, `/history`, `/settings`, `/mywords`, `/study`,
`/delete_me`, плюс URL/API-key/callback-входы) и «Раскатка новой
версии». Версия Go подтянута к `1.26.2`.

## 29 — hardening
Прод-готовность: `recoverMiddleware` ловит панику любого хендлера,
логирует stacktrace с `errors_total=1` и отвечает пользователю
`error.generic`. Groq-клиент ретраит 5xx и сетевые ошибки 2 раза с
паузами 1s/3s (4xx — без ретраев), новые тесты покрывают
успех-после-ретраев, исчерпание ретраев и no-retry-on-401. Graceful
shutdown через `Bot.Shutdown(30s)` поверх `sync.WaitGroup`-обёртки
вокруг хендлеров. Лог-ключи приведены к спеке: `analysis_duration_ms`,
`tokens_estimated`, `cache_hit`, `groq_retries`, `errors_total`.
README пополнился разделами «Бэкап `bot.db`», «Graceful shutdown» и
«Observability».

## 27.5 — refactor-estimate-tokens
Дубль эвристики `runes/4` устранён: единственная реализация
`EstimateTokens` живёт в `internal/llm`, а `articles.AnalyzeArticle`
и тесты `internal/articles/long_test.go` импортируют её оттуда.
`make build` и `make test` зелёные.

## 28 — tests
Доменные пакеты подтянуты к ≥70% покрытия: добавлены unit-тесты на
`users.codes` (`IsCEFR`, `CEFRShift`, `IsSupportedLearningLanguage`),
sqlite-репо `user_languages` (полный жизненный цикл Set/Activate/
SetCEFR/Active/List) и `users.Repository`
(Create/ByTelegramID/UpdateInterfaceLanguage). Появился пакет
`internal/llm/mock` с JSON-фикстурами `analyze_clean` /
`analyze_flagged` / `adapt_clean` (фикстуры валидируются через
реальную JSON-схему, чтобы не разъезжались с прод-контрактом). Тест
`internal/i18n` сравнивает наборы ключей `en`/`ru`/`es` —
расхождения считаются ошибкой. `make test` зелёный, полный прогон ~4с.

## 27 — content-safety
Появилась статическая защита от adult/illegal-источников: файл
`configs/blocked_domains.txt` встраивается через `embed`, парсится
в `articles.Blocklist` (поддомены тоже матчатся, регистр и
комментарии `#` игнорируются). Проверка домена вшита в
`AnalyzeArticle` ДО вызова экстрактора (лог
`extractor_called=false`); после ответа LLM непустой `safety_flags`
ведёт к `ErrBlockedContent` без записи в `articles`. В URL-handler
добавлены i18n-сообщения `article.err.blocked_source` и
`article.err.blocked_content`. Юнит-тесты покрывают парсинг
блок-листа, поддоменный матч, отсутствие сетевого вызова и пустой
articles после safety-флага.

## 26 — long-articles
Перед обращением к Groq статьи теперь проходят токен-гейт: эвристика
`EstimateTokens = ⌈runes/4⌉` сравнивается с
`MAX_TOKENS_PER_ARTICLE` (дефолт 7000). При превышении возвращаем
типизированный `*TooLongError` (с `Tokens/Words/Limit`), пишем в лог
`tokens_estimated/words_estimated/reason=exceeds_token_budget` и шлём
пользователю `article.err.too_long` с приблизительным числом слов;
LLM в этом случае не вызывается. Граничный случай (estimate == limit)
проходит. Покрыто sqlite-тестами на отказ, границу и работу
дефолтного лимита.

## 25 — rate-limit
В транспортном слое появился per-user token-bucket
`internal/telegram/ratelimit.go` (capacity=`RATE_LIMIT_PER_HOUR`,
дефолт 10; рефилл `1h / capacity`). Лимит применяется только к
URL-handler (`/start`, settings и прочее свободны); при превышении
шлём `article.err.rate_limited` с числом минут до следующего слота.
Покрыто юнит-тестами на исчерпание, частичный/полный рефилл,
изоляцию между пользователями и nil-safe вызовы.

## 24 — delete-me
Появилась команда `/delete_me` и callback-семейство `del:*` —
GDPR-удаление с обязательным подтверждением. На «Да, удалить» одна
транзакция вычищает строки пользователя из `users`, `user_languages`,
`user_api_keys`, `articles` (с каскадом на `article_words`) и
`user_word_status`; `dictionary_words` остаются нетронутыми, FSM
(онбординг, ожидание API-ключа, текущая study-сессия) сбрасываются.
Кнопка «🗑 Удалить мои данные» в `/settings` теперь ведёт в тот же
flow вместо плейсхолдера. Покрыто sqlite-тестом, проверяющим, что
второй пользователь и shared-словарь после удаления остаются на месте.

## 23 — categories
LLM-категория статьи теперь нормализуется до одного из 9 канонических
кодов (Travel/Tech/Politics/Sports/Health/Culture/Business/Science/
Other) через `articles.NormalizeCategory`; невалидное значение → Other,
не падаем. В article card отображается локализованная категория, в
History появилась строка фильтров (All + 9 категорий) с пагинацией
внутри выбранного фильтра. Добавлена seed-миграция `0006_seed_
categories` и методы `ListByUserAndCategory` / `CountByUserAndCategory`.

## 22 — flashcards
Появилась команда `/study` и callback-семейство `study:*` —
flashcard-сессии по словам в статусе `learning`. FSM в
`internal/session/study.go` ведёт колоду, счётчики и список
освоенных лемм; репозиторий получил `LearningQueue`,
`SampleArticleWords`, `RecordCorrect` (с порогом 3 для перевода в
`mastered`) и `RecordWrong`. Тесты на FSM и DB-уровень покрывают
«3 подряд → mastered» и «ошибка обнуляет streak».

## 21 — my-words
Появилась команда `/mywords`: inline-меню со словарём пользователя по
активному языку с фильтрами All / Learning / Known / Mastered и
пагинацией по 10. Каждое слово открывается в подменю смены статуса
(`mw:e`/`mw:s`); под капотом добавлены `dictionary.UserWordEntry` +
методы `CountUserWords`/`PageUserWords` и i18n-строки `mywords.*`
для ru/en/es.

## 20.6 — refactor-cefr-and-lang-validators
Списки поддерживаемых языков и уровней CEFR + соответствующие
валидаторы переехали в новый `internal/users/codes.go`
(`SupportedInterfaceLanguages`, `SupportedLearningLanguages`,
`CEFRLevels`, `IsSupportedInterfaceLanguage`, `IsSupportedLearningLanguage`,
`IsCEFR`, `CEFRShift`); дубли в `articles`, `onboarding.go` и
`settings.go` удалены, onboarding-callback'и теперь используют
верхний регистр (`onb:level:A1..C2`).

## 20.5 — refactor-open-article-helper
Три копии «открыть карточку статьи»-флоу (`hist:open`, `art:`-callback
и cache-hit ветка url-handler-а) сведены в общий `cardRenderer` в
`internal/telegram/handlers/card_render.go`: load → ownership → count
→ preview → ensure-adapted (regen + reload) → render. Url-handler
теперь тоже показывает локализованное сообщение при ошибке regen
(раньше писал только в лог), а тонкая обёртка `articles.Service.
ArticleByID` удалена за ненадобностью.

## 20 — settings-menu
Появилась команда `/settings` и callback-семейство `set:*` —
inline-меню с пятью пунктами: язык интерфейса (ru/en/es), активный
изучаемый язык, уровень CEFR, API-ключ и заглушка для удаления данных
(до шага 24). Под капотом добавлены `users.Repository.UpdateInterface
Language` + `users.Service.SetInterfaceLanguage`, а
`UserLanguageRepository` обзавёлся `List`, `Activate` и `SetCEFR`.
Смена интерфейсного языка отражается сразу: confirmation
рендерится новым localizer-ом. Добавление нового изучаемого языка
ведёт к под-меню выбора CEFR; пункт «API-ключ» переводит в
существующий setkey-flow через `session.APIKeyWaiter`. i18n-строки
`settings.*` добавлены в ru/en/es.

## 19 — regen-on-level-change
Поле `articles.adapted_versions` переехало с относительной модели
`{lower,current,higher}` на абсолютную карту по CEFR (`{"A1":"…", …,
"C2":"…"}`); LLM-анализ конвертирует относительные уровни в
абсолютные через новый `articles.CEFRShift` с использованием
текущего `cefr_level` пользователя. Кэш статьи теперь срабатывает
независимо от уровня: старая статья переиспользуется и догенерируется
лениво. Добавлен мини-промпт `llm.Adapt` (`adapt_system.txt` +
`adapt_user.tmpl` + `schema/adapt.json`) и `articles.Service.Adapt`,
который выбирает ближайший по CEFR существующий source-text, зовёт
LLM, мержит ответ в `adapted_versions` через
`Repository.UpdateAdaptedVersions`. URL/History/Card-handler-ы
проверяют наличие нужного абсолютного уровня и при его отсутствии
показывают статус «🪄 Адаптирую под `<level>`…», после чего
перерисовывают карточку. i18n обновлён в ru/en/es; добавлен тест
`TestAdapt_FillsMissingLevelAndCachesIt` (LLM зовётся один раз,
повторный запрос — cache hit) и заменён старый тест
fall-through-на-CEFR-mismatch.

## 18 — adapted-summary
Article card теперь поддерживает три уровня адаптации текста и
переключение языка summary без отправки нового сообщения. Добавлен
`articles.AdaptedVersions` + `Article.ParseAdaptedVersions`,
inline-клавиатура card-а получила ряд из трёх кнопок «Проще /
Текущий / Сложнее» (недоступные уровни остаются с noop-callback,
выбранный отмечается «✓») и тоггл «🌐 Перевод/Оригинал summary».
Состояние просмотра кодируется прямо в callback-данных
(`art:v:<id>:<l|c|h>:<t|n>`), новый `Card`-handler через
`EditMessageText` перерисовывает карточку. URL- и History-флоу
открывают карточку в дефолтном виде (`current` + target summary),
i18n обновлён в ru/en/es, добавлен round-trip-тест на
`parseCardCallback`.

## 17 — reuse-articles
В `articles.Service.AnalyzeArticle` появилась проверка кэша: до
вызова extractor-а и LLM рассчитываем `URLHash(NormalizeURL(url))`,
смотрим в `articles_repo.ByUserAndHash` (новый метод), и если
найдена запись с тем же `language_code` и `cefr_detected ==
active.CEFRLevel` — возвращаем сохранённую article card,
переподнимая слова через `awords.PageByArticle`. В логах появляется
`cache_hit=true` с `analysis_skipped_ms`. Случай несовпадающего
CEFR пока проваливается в обычный путь (зафиксировано в тесте,
шаг 19 будет это улучшать). Покрыто двумя тестами: cache hit (LLM
не зовётся) и fall-through на CEFR mismatch.

## 16 — history
Появилась команда `/history` и callback-семейство `hist:*`: из
`articles_repo` отдаются 10 статей пользователя на страницу
(`ListByUser`/`CountByUser`, ORDER BY `created_at DESC`), кнопка
по статье открывает сохранённую article card без повторного
вызова LLM, рендер теперь живёт в общем `handlers/card.go`
(используется и в URL-флоу). Добавлены i18n-строки `history.*`
для ru/en/es и репо-тест на пагинацию + изоляцию по user_id.

## 15.6 — refactor-remove-unused-withtx
Удалён мёртвый метод `(*sqliteRepo).WithTx(ctx, fn) error` из
`internal/articles/repo.go` — он не входил в интерфейс `Repository`,
не использовался и просто дублировал свободную функцию
`articles.WithTx(ctx, db, fn)`. Семантика свободной функции не
изменилась, `make build` и `make test` — зелёные.

## 15.5 — refactor-callback-user-resolve
В `internal/telegram/handlers/helpers.go` появились две свободные
функции `resolveCallbackUser` и `resolveMessageUser`, которые
выполняют общий блок `users.Service.RegisterUser` + логирование
ошибки. Шесть мест в `onboarding.go`/`url.go`/`apikey.go`/`words.go`
переписаны на этот хелпер; приватный `(*Words).resolveUser` удалён,
локализация осталась за вызывающей стороной (`tgi18n.For(bundle,
u.InterfaceLanguage)`), `make build` и `make test` — зелёные.

## 15 — word-status
Под каждым словом в paginated-просмотре появились три inline-кнопки
«Знаю / Учу / Пропустить» (callback `wstat:<article_id>:<page>:<word_id>:<status>`),
которые upsert-ят `user_word_status` и перерисовывают сообщение —
выбранная кнопка получает «✓», остальные остаются кликабельными,
чтобы пользователь мог переключить статус. В `articles.Service.AnalyzeArticle`
добавлен сбор лемм со статусами `known`/`mastered` через новый
`UserWordStatusRepository.KnownLemmas`; они уезжают в `llm.AnalyzeRequest.KnownWords`,
поэтому при повторном анализе статьи на том же языке LLM не предлагает
уже известные слова. Также добавлен `GetMany` для рендера статусов
страницы одним запросом, тесты `KnownLemmas`/`GetMany`/интеграционный
тест проброса `KnownWords` через mock-LLM, а локали ru/en/es обновлены
строками кнопок и тостов.

## 14 — deploy-v01
Walking Skeleton поднят на mac mini под cron-watchdog. Добавлен
`scripts/linguine-watchdog.sh` (адаптация `boltun-watchdog.sh`,
параметризованная через `LINGUINE_DIR`/`LINGUINE_LOG`, матч по
`bin/tg-linguine` чтобы не ловить сам watchdog по подстроке имени);
секция «Деплой на mac mini» в README с генерацией `ENCRYPTION_KEY`,
бэкапом мастер-ключа, крон-строкой (`~/Projects/tg-linguine` через
`/bin/sh`-tilde) и smoke-чеклистом (`/start` → онбординг → Groq-key →
URL, `kill -9` → автоподъём ≤1 мин, reboot). `watchdog.log*` в
`.gitignore`. Бот живёт в репо-директории, никаких rsync/scp.

## 13 — pagination-words
Завершён Walking Skeleton: из article card по кнопке «Показать все
слова» открывается inline-просмотр по 5 слов за страницу с переводом,
транскрипцией и парой примеров (target/native). Добавлены
`ArticleWordsRepository.CountByArticle` + `PageByArticle` (JOIN
`article_words` × `dictionary_words`, ORDER BY rowid) и handler
`handlers.Words` на callback `words:<article_id>:<page>` с
проверкой owner-а статьи, кнопками «◀ Prev / Next ▶ / Закрыть» и
обработкой крайних страниц через `noop`-callback. i18n-строки
добавлены в ru/en/es; `Close` удаляет сообщение.

## 12 — analyze-article-usecase
Сквозной сценарий «URL → article card»: `articles.Service.AnalyzeArticle`
получает активный язык/уровень и расшифрованный Groq-ключ, извлекает
статью через `Extractor`, зовёт `llm.Provider.Analyze` и атомарно
пишет articles + dictionary_words + article_words + user_word_status в
одной транзакции, попутно логируя `article_chars`, `words_count`,
`duration_ms`. В Telegram добавлен handler `handlers.URL` с regex-
матчем на http(s)-ссылку, тремя промежуточными статусами через
`EditMessageText` («🔎/🧠/💾») и финальной article card (заголовок,
определённый CEFR, summary, превью 5 слов, кнопка «Показать все
слова» с callback `words:<id>:0` для шага 13). Ошибки маппятся на
понятные i18n-сообщения (`article.err.*` + `apikey.*`).

## 11 — articles-words-repos
Добавлен persistence-слой результата анализа: миграция
`0005_articles_and_words` (categories, articles, dictionary_words,
article_words, user_word_status), доменные типы и SQLite-репозитории
`internal/articles` (Article + UpsertCategory) и `internal/dictionary`
(DictionaryRepository, ArticleWordsRepository, UserWordStatusRepository).
Все вставки можно атомарно выполнить через `articles.WithTx` —
подтверждено тестами (rollback, дедупликация лемм по language_code+lemma,
upsert статуса слова). Прежний extracted-тип `articles.Article`
переименован в `Extracted`, чтобы освободить имя под доменную сущность.

## 10 — llm-prompt-schema
Описан контракт «статья → структурированный JSON»: системный/пользовательский
промпты в `internal/llm/prompts/*` (embed), JSON-Schema `analysis.json`
(summary/category/CEFR/adapted_versions/words/safety_flags) и метод
`groq.Client.Analyze` c `response_format: json_object`, моделью из
`GROQ_MODEL` (дефолт `llama-3.3-70b-versatile`) и одним retry при невалидном
ответе. Покрыто фикстурными тестами на схему (валид/missing/wrong type/bad
enum) и httptest-тестами на retry, 401 и 429.

## 09 — article-extraction
Добавлен пакет `internal/articles`: интерфейс `Extractor`, реализация на
`go-shiori/go-readability` с HTTP-таймаутом и жёстким лимитом на размер
тела (`MAX_ARTICLE_SIZE_KB`), нормализация URL (utm_*/fbclid/gclid/fragment,
lower-case host, без trailing slash) и `URLHash = sha256(normalized) hex`.
Различаем `ErrNetwork`/`ErrTooLarge`/`ErrNotArticle`/`ErrPaywall`; покрыто
unit-тестами + golden-тестом на локальной HTML-фикстуре через httptest.

## 08 — groq-api-key
Подключили хранение Groq API-ключа: миграция `0004_user_api_keys`,
AES-256-GCM в `internal/crypto`, `llm.Provider` + Groq-клиент с
классификацией ошибок (401/429/сеть), `users.APIKeyRepository` с
upsert через шифрование, команда `/setkey`, удаление сообщения с
ключом из чата и заметка про мастер-ключ в README/`.env.example`.
Логи не содержат значение ключа — только `user_id` и причину.

## 07 — onboarding
Добавлен мини-wizard выбора языка изучения и CEFR-уровня: миграция
`0003_user_languages`, in-memory FSM в `internal/session` с TTL 30 минут,
SQLite-репо `UserLanguageRepository.Set/Active` и inline-кнопки с
callback'ами `onb:lang:*` / `onb:level:*`. `/start` запускает wizard, если
у пользователя ещё нет активного языка, и здоровается обычным образом, если
язык уже выбран; незавершённый шаг можно продолжить повторным `/start`.

## 06 — user-repo
Появился пакет `internal/users` с доменной структурой `User`, SQLite-репозиторием
и идемпотентным use case `RegisterUser`; миграция `0002_users_extend` добавляет
`interface_language`, `telegram_username`, `first_name`, `updated_at`, а
`/start` теперь регистрирует пользователя при первом обращении и логирует флаг
`created`.

## 05.5 — refactor-i18n-bundle
Заменили загрузку локалей через `init()` с `panic` на явный конструктор
`i18n.NewBundle() (*i18n.Bundle, error)`; bundle теперь прокидывается из
`main.go` в `telegram.New`, а ошибка чтения YAML возвращается наружу.
