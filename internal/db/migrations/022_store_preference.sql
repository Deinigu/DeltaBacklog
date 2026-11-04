-- Deals: preferred stores. The ranked list of shops and the threshold live in `setting` ('deals_stores' as a JSON array
-- of shop names, 'deals_store_threshold' as a percent), so no table is needed for them.
--   deal_price / wish_price   cur_* is now the price at the preferred shop (the cheapest offer when no shop is ranked);
--                             alt_* is a clearly cheaper offer at another shop (NULL when there is none), shown as a hint.
--                             Alerts follow cur_*, never alt_*
ALTER TABLE deal_price ADD COLUMN alt_shop TEXT;
ALTER TABLE deal_price ADD COLUMN alt_price INTEGER CHECK (alt_price >= 0);
ALTER TABLE deal_price ADD COLUMN alt_url TEXT CHECK (alt_url IS NULL OR alt_url LIKE 'http://%' OR alt_url LIKE 'https://%');
ALTER TABLE wish_price ADD COLUMN alt_shop TEXT;
ALTER TABLE wish_price ADD COLUMN alt_price INTEGER CHECK (alt_price >= 0);
ALTER TABLE wish_price ADD COLUMN alt_url TEXT CHECK (alt_url IS NULL OR alt_url LIKE 'http://%' OR alt_url LIKE 'https://%');
