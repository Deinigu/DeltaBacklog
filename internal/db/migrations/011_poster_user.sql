-- A poster the user chose (or pasted) is theirs: the background queue and `fetch-posters --upgrade` never replace it.
-- poster_src cannot say this: its CHECK ('igdb', 'sgdb', 'crop') cannot be altered in place, and a chosen image
-- still came from one of those places. So a separate flag; 1 = chosen by the user, 0 = made automatically.
ALTER TABLE game ADD COLUMN poster_user INTEGER NOT NULL DEFAULT 0 CHECK (poster_user IN (0, 1));
