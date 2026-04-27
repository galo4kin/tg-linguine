# Шаг 10. LLM-промпт и schema-валидация (5–7h)

## Цель
Контракт «текст статьи → структурированный JSON». LLM возвращает
валидный JSON, проходящий schema-валидацию.

## Контекст
Provider: Groq, OpenAI-compatible JSON mode. Schema-валидация:
`github.com/santhosh-tekuri/jsonschema/v5`. Лимит контекста — 8k
токенов, промпт + статья должны влезать.

## Действия
1. `internal/llm/prompts/system.txt` — системный промпт на английском:
   роль ассистента, требование вернуть строго JSON по схеме,
   запрет на пояснения/markdown.
2. `internal/llm/prompts/user.tmpl` — Go-шаблон с подстановкой:
   - `{{.TargetLanguage}}` (изучаемый),
   - `{{.NativeLanguage}}` (родной),
   - `{{.CEFR}}`,
   - `{{.KnownWords}}` (slice, может быть пуст — учитывается в
     шаге 15),
   - `{{.ArticleTitle}}`, `{{.ArticleText}}`.
3. `internal/llm/schema/analysis.json` — JSON-schema со всеми
   полями результата:
   ```
   summary_target, summary_native, category, cefr_detected,
   adapted_versions: { lower, current, higher },
   words: [{ surface_form, lemma, pos, transcription_ipa,
            translation_native, example_target, example_native }],
   safety_flags: [string]
   ```
4. `internal/llm/groq/analyze.go`: метод `Analyze(ctx, key, req
   AnalyzeRequest) (AnalyzeResponse, error)`.
   - `response_format: {type: "json_object"}`,
   - модель — параметр конфига `GROQ_MODEL`, дефолт
     `llama-3.3-70b-versatile` (актуализировать с пользователем,
     если поменялось),
   - валидация ответа против schema,
   - 1 retry при невалидном JSON: добавить в промпт error message и
     попросить переотдать. Дальше — ошибка наружу.
5. Token counter — простая эвристика `len(text)/4` для предпроверки;
   точный счётчик (например, `tiktoken-go`) — опционально, для шага
   26.
6. Юнит-тест: фикстура валидного JSON проходит, фикстура с
   нарушением schema — отклоняется.

## DoD
- На фикстурном тексте «Lorem ipsum...»-стиля получен JSON с
  заполненными полями.
- Schema-валидация ловит: missing field, wrong type, bad enum.
- Невалидный JSON от LLM → один retry → если снова невалидно,
  ошибка наружу.
- Стоимость теста: реальный hit к Groq разрешён (по ключу
  пользователя), но фикстуры — без сети.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 8.
