-- name: UpgradeUser :one
UPDATE users 
SET
    is_chirpy_red = TRUE
WHERE users.id = $1
RETURNING *;