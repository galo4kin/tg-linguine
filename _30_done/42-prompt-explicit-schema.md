# Step 42 — prompt-explicit-schema

## Проблема

После step 41 retry-after работает, но всплыла глубже лежащая проблема в логе step 39 (schema-retry): модель отдаёт неверный shape JSON — top-level поля `title`, `article_text`, `lower/current/higher` вместо требуемых `summary_target`, `summary_native`, `adapted_versions: {lower, current, higher}` и т.д.

System-промпт говорит «matches the schema provided to you», но реальная схема в нём не процитирована, а Groq в режиме `response_format: json_object` её на сервере не енфорсит. Модель путает входной шаблон с выходным.

## Что делаем

1. Дописать в `internal/llm/prompts/system.txt` явный «output schema sketch» — буквальный пример JSON со всеми обязательными ключами и типами. Это снимает у модели догадки про имена полей.
2. Пометить блоки в `internal/llm/prompts/user.tmpl` как INPUT (`<<<INPUT>>>` / `<<<END INPUT>>>`), чтобы модель не путала input-метки с output-ключами.
3. Обновить (если нужно) тестовые fixture'ы, проверяющие RenderUserPrompt, на новые границы.

## DoD

- `make build && make test` зелёные.
- Один коммит `step 42: prompt-explicit-schema`.
- `pkill -f bin/tg-linguine` → новый PID.
- Wikipedia AI: truncate → `groq.chat ok` → JSON проходит схему → карточка.
