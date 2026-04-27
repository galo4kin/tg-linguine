# Releases

## 05.5 — refactor-i18n-bundle
Заменили загрузку локалей через `init()` с `panic` на явный конструктор
`i18n.NewBundle() (*i18n.Bundle, error)`; bundle теперь прокидывается из
`main.go` в `telegram.New`, а ошибка чтения YAML возвращается наружу.
