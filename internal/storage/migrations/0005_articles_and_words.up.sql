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
  adapted_versions TEXT,
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
  status TEXT NOT NULL,
  correct_streak INTEGER NOT NULL DEFAULT 0,
  correct_total INTEGER NOT NULL DEFAULT 0,
  wrong_total INTEGER NOT NULL DEFAULT 0,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, dictionary_word_id)
);
