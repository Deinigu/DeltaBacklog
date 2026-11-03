-- Deals: games the owner hid from the Deals page. A hidden game is not shown and not refreshed (no IsThereAnyDeal calls);
-- its stored match and prices are kept. Hiding is reversible and cascades when the game is deleted.
CREATE TABLE deal_hidden (
    game_id   INTEGER PRIMARY KEY REFERENCES game(id) ON DELETE CASCADE,
    hidden_at TEXT NOT NULL
) STRICT;
