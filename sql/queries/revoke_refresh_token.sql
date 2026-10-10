-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET 
    updated_at = NOW(),
    revoked_at = NOW()
WHERE token = $1;