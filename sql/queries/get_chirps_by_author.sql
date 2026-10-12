-- name: GetChirpsByAuthor :many
SELECT * FROM chirps
WHERE chirps.user_id = $1
ORDER BY created_at ASC;