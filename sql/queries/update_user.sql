-- name: UpdateUser :one
UPDATE users 
SET
    updated_at = NOW(),
    email = $1,
    hashed_password = $2
WHERE users.id = $3
RETURNING *;