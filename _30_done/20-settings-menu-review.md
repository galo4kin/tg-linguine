# Code review после шага 20

Прошлый review был на шаге 15. С тех пор закрыты шаги 16, 17, 18, 19,
20 и два refactor-step (15.5, 15.6). Ниже — что бросилось в глаза в
изменённом коде на стыке `articles` ↔ `telegram/handlers`.

## Найдено

### 1. Дублирование «открыть сохранённую статью» в трёх местах
Шаги 16/17/18/19 нагрузили карточку статьи новым поведением (level
buttons, summary toggle, regen-on-missing-level), и теперь
`telegram/handlers/card_handler.go::HandleCallback`,
`telegram/handlers/history.go::openArticle` и
`telegram/handlers/url.go::Handle` (ветка после cache hit / после
анализа) делают одно и то же: достать `Article`, проверить владельца,
посчитать слова, поднять preview-леммы, прочитать активный язык
пользователя, при отсутствии нужного абсолютного CEFR-уровня
показать статус «🪄 Адаптирую под X…», вызвать `articles.Service.
Adapt`, перезагрузить article и сделать `EditMessageText` с
`renderArticleCard` + `articleCardKeyboard`. Это ~50–70 строк почти
идентичного кода в каждом из трёх мест плюс расхождения: card_handler
прячет ошибку под обновление одной строки, history делает то же самое,
а url-flow (cache hit) пишет ошибку только в лог — пользователь видит
пустую карточку. Выделить общий хелпер
`renderStoredArticle(ctx, b, chatID, msgID, loc, deps, view, article,
previewLemmas)` (или метод на новом `cardRenderer` в `handlers/`)
— получим единственное место, где живёт цепочка regen→reload→render.
Вынесем в отдельную refactor-задачу.

### 2. Три валидатора CEFR / языка не знают друг про друга
В `handlers/onboarding.go` лежит `validLanguage` (ru/en/es) и
`validLevel` (a1..c2 в нижнем регистре), в `handlers/settings.go` — те
же по набору `validInterfaceLanguage`, `validLearnableLanguage` (тоже
ru/en/es) и `validCEFR` (A1..C2 в верхнем регистре), а в
`articles/article.go` — `IsCEFR` (A1..C2). Разные регистры, разные
slice'ы, всё дублируется. Стоит свести: `articles.IsCEFR` уже
есть, для языков завести `users.IsSupportedInterfaceLanguage` /
`users.IsSupportedLearningLanguage` (рядом с `NormalizeLanguage`),
а в settings/onboarding оставить только нормализацию регистра. Тоже
вынесем в refactor-задачу.

### 3. Service.ArticleByID добавлен ad-hoc только под url.go
`articles.Service.ArticleByID` появился в шаге 19 как тонкая обёртка
вокруг `articles.Repository.ByID`, потому что url-handler уже зависит
от `Service`, а не от репо. При этом `card_handler.go` и `history.go`
по-прежнему ходят в `Repository.ByID(ctx, db, id)` напрямую. Если
сделаем хелпер из пункта 1, эта несостыковка исчезнет (хелпер сам
выберет один путь). Отдельная задача не нужна — тащим вместе с #1.

### 4. `articles/article.go` теперь содержит CEFR-утилиты
Файл `internal/articles/article.go` после шага 19 содержит и доменный
тип `Article`, и `AdaptedVersions map[string]string`, и общие CEFR
константы (`CEFRLevels`, `IsCEFR`, `CEFRShift`). Чисто-поэтической
нагрузки не несёт, но `package-by-feature` начинает протекать: CEFR-
утилиты используют settings/onboarding/handlers. Не критично — можно
оставить как есть, либо в той же refactor-задаче (#2) переехать на
`internal/cefr`. Помечаю как наблюдение, отдельный todo не создаю.

## Что неблокирующего, но стоит держать в уме
- `settings.go` довольно длинный (≈400 строк). Внутри он логически
  разделён по «sub-menu», и пока ок. Если меню вырастет, имеет смысл
  разнести по файлам (top, iface, lang, cefr, apikey, delete) или на
  state-driven подход.
- `regenerateAndReload` в `card_handler.go` вернёт `nil` на ошибку и
  опирается на то, что caller bail-нет. После рефактора (#1) этот
  шаблон унифицируется.

## Создаются refactor-задачи
- `20.5-refactor-open-article-helper.md` — вытащить общий «open
  stored article card» из card_handler / history / url.
- `20.6-refactor-cefr-and-lang-validators.md` — сдедуплицировать
  валидаторы CEFR и кодов языка.
