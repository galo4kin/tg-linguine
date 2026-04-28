# Шаг 21. My words — глобальный словарь пользователя (4–5h)

## Цель
Меню «My words» — все слова со статусами `learning` и `known` по
активному языку.

## Действия
1. Команда `/mywords` (или кнопка).
2. Запрос: `user_word_status JOIN dictionary_words` фильтр по
   `language_code = active`.
3. Фильтры: All / Learning / Known / Mastered. Inline-кнопки сверху.
4. Пагинация по 10 слов.
5. Для каждого — surface (lemma), статус, кнопка «изменить статус».

## DoD
- Список выводится с пагинацией и фильтрами.
- Смена статуса из «My words» влияет на запись.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 15.
