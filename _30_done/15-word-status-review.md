# Код-ревью на шаге 15

Окно ревью: коммиты от шага 6 (после ревью на шаге 5) до шага 15
включительно.

## Что просмотрено

- `internal/articles/{usecase,repo,article,extractor,url}.go` и тесты;
- `internal/dictionary/{repo,word,page_test,repo_test}.go`;
- `internal/llm/{provider,analysis}.go` и `groq/*`;
- `internal/users/{usecase,repository,sqlite,languages,apikey_repo}.go`;
- `internal/telegram/bot.go` и все хендлеры в
  `internal/telegram/handlers/`;
- `cmd/bot/main.go`;
- i18n-локали `en.yaml`, `ru.yaml`, `es.yaml`;
- миграции `0001..0005`.

## Что хорошо

- Чёткое package-by-feature разделение, никаких циклических импортов.
- Транзакционная атомарность сохранения статьи (`articles.WithTx` +
  `sql.Tx`) — есть юнит-тест на откат.
- Все callback-префиксы вынесены в константы в одном пакете (handlers),
  совпадений и magic-строк нет.
- LLM-схема валидируется детерминированно (`jsonschema/v5`), есть
  testdata-фикстуры и тест на retry-сценарии.
- `KnownWords` пробрасывается из БД в шаблон промпта, тест на
  «повторный анализ не предлагает known-слова» поверх mock-LLM.
- i18n покрытие синхронно по трём языкам.

## Найденные точки рефакторинга

### 1. Дублирование boilerplate `RegisterUser` в callback-хендлерах
Файлы: `internal/telegram/handlers/onboarding.go` (×2),
`internal/telegram/handlers/words.go` (×1, инкапсулировано в
`resolveUser`), `internal/telegram/handlers/url.go` (×1),
`internal/telegram/handlers/apikey.go` (×2).

Каждый хендлер 4–5 строками собирает `users.TelegramUser` и вызывает
`RegisterUser`, теряет `created`, логирует ошибку, идёт дальше. Логика
одинакова. Стоит вынести в общий хелпер `handlers.resolveCallbackUser`
/ `handlers.resolveMessageUser`, который возвращает `*users.User` и
`*goi18n.Localizer`. См. отдельный таск.

### 2. Мёртвый метод `(*sqliteRepo).WithTx` в `internal/articles/repo.go`
Метод `(*sqliteRepo) WithTx(ctx, fn) error` (строка ~109) нигде не
вызывается — все клиенты используют пакетную функцию
`articles.WithTx(ctx, db, fn)`. Метод дублирует функцию, мешает
интерфейсу `Repository` (он не входит в интерфейс). См. отдельный
таск на удаление.

### 3. (не делаем) Два одинаковых `DBTX` интерфейса
`articles.DBTX` и `dictionary.DBTX` — структурно один и тот же
3-метод-минимум `*sql.DB`/`*sql.Tx`. Можно вынести в общий пакет
(например `internal/storage`), но малые интерфейсы по месту — это
идиоматичный Go, и дублирование пока не мешает читаемости. Не заводим
таск; вернёмся, если появится третий пакет с тем же интерфейсом.

### 4. (не делаем) `discardWriter` дублируется в test-пакетах
`internal/articles/usecase_test.go` и
`internal/dictionary/repo_test.go` имеют по своей копии
`discardWriter`. Это test-only мелочь, не заводим.

## Заведено таск-файлов

- `_10_todo/15.5-refactor-callback-user-resolve.md`
- `_10_todo/15.6-refactor-remove-unused-withtx.md`

Оба будут забраны следующим запуском раньше `16-history.md`, потому
что префикс `15.5/15.6` сортируется до `16`.
