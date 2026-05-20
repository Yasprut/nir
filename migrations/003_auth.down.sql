ALTER TABLE policies
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS submitted_by,
    DROP COLUMN IF EXISTS reviewed_by,
    DROP COLUMN IF EXISTS review_comment,
    DROP COLUMN IF EXISTS submitted_at;

DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
