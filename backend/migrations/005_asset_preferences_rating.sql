ALTER TABLE asset_preferences
ADD COLUMN IF NOT EXISTS rating INT NOT NULL DEFAULT 0;

UPDATE asset_preferences
SET rating = 0
WHERE rating < 0 OR rating > 5;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'asset_preferences_rating_range'
      AND conrelid = 'asset_preferences'::regclass
  ) THEN
    ALTER TABLE asset_preferences
    ADD CONSTRAINT asset_preferences_rating_range CHECK (rating BETWEEN 0 AND 5);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_asset_preferences_rating
ON asset_preferences (rating, asset_id);
