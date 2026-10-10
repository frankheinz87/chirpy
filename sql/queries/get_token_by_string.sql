-- name: GetTokenByString :one
SELECT * FROM refresh_tokens
WHERE token = $1;