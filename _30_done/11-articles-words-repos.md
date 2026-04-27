# Шаг 11. Доменные сущности и репозитории статей/слов (5–6h)

## Цель
Persistence-слой для результата анализа: статьи, общий словарь,
связь статья↔слова, статусы слов пользователя, категории.

## Контекст
Сохранение — атомарно: всё или ничего, через `sql.Tx`.

## Действия
1. Миграция `0005_articles_and_words.up/down.sql`:
   ```sql
   CREATE TABLE categories (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     code TEXT NOT NULL UNIQUE
   );

   CREATE TABLE articles (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
     source_url TEXT NOT NULL,
     source_url_hash TEXT NOT NULL,
     title TEXT NOT NULL,
     language_code TEXT NOT NULL,
     cefr_detected TEXT,
     summary_target TEXT,
     summary_native TEXT,
     adapted_versions TEXT,             -- JSON; шаг 18
     category_id INTEGER REFERENCES categories(id),
     created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     UNIQUE(user_id, source_url_hash, language_code)
   );
   CREATE INDEX idx_articles_user ON articles(user_id);

   CREATE TABLE dictionary_words (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     language_code TEXT NOT NULL,
     lemma TEXT NOT NULL,
     pos TEXT,
     transcription_ipa TEXT,
     UNIQUE(language_code, lemma)
   );

   CREATE TABLE article_words (
     article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
     dictionary_word_id INTEGER NOT NULL REFERENCES dictionary_words(id),
     surface_form TEXT NOT NULL,
     translation_native TEXT,
     example_target TEXT,
     example_native TEXT,
     PRIMARY KEY (article_id, dictionary_word_id, surface_form)
   );

   CREATE TABLE user_word_status (
     user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
     dictionary_word_id INTEGER NOT NULL REFERENCES dictionary_words(id),
     status TEXT NOT NULL,             -- learning|known|skipped|mastered
     correct_streak INTEGER NOT NULL DEFAULT 0,
     correct_total INTEGER NOT NULL DEFAULT 0,
     wrong_total INTEGER NOT NULL DEFAULT 0,
     updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     PRIMARY KEY (user_id, dictionary_word_id)
   );
   ```
2. Пакеты:
   - `internal/articles/repo.go` (Article + ArticleRepository).
   - `internal/dictionary/repo.go` (DictionaryRepository,
     UserWordStatusRepository, ArticleWordsRepository).
3. `Save(ctx, tx, ...)` — все вставки одной транзакцией. Для
   `dictionary_words` — `INSERT ... ON CONFLICT DO NOTHING RETURNING
   id` (или select-then-insert).
4. Юнит-тесты на репо: открываем sqlite в tmpfile, прогоняем
   миграции, сохраняем, читаем обратно.

## DoD
- Все таблицы создаются миграцией.
- Транзакционность: при ошибке в середине save — ничего не
  сохраняется (юнит-тест).
- `dictionary_words` дедуплицируется по (language_code, lemma).
- `make build` + `make test` зелёные.

## Зависимости
Шаг 6.
