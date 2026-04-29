CREATE TABLE active_screen (
    chat_id      INTEGER PRIMARY KEY,
    message_id   INTEGER NOT NULL,
    screen_id    TEXT    NOT NULL,
    context_json TEXT    NOT NULL DEFAULT '{}',
    updated_at   INTEGER NOT NULL
);
