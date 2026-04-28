# Releases

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
