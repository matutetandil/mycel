package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore implements SessionStore using Redis
type RedisSessionStore struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisSessionStore creates a new Redis session store
func NewRedisSessionStore(client *redis.Client, keyPrefix string) *RedisSessionStore {
	if keyPrefix == "" {
		keyPrefix = "mycel:auth:session:"
	}
	return &RedisSessionStore{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// sessionKey returns the Redis key for a session
func (s *RedisSessionStore) sessionKey(id string) string {
	return s.keyPrefix + id
}

// userSessionsKey returns the Redis key for user's session list
func (s *RedisSessionStore) userSessionsKey(userID string) string {
	return s.keyPrefix + "user:" + userID
}

// Create stores a new session in Redis
func (s *RedisSessionStore) Create(ctx context.Context, session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Calculate TTL from ExpiresAt
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("session already expired")
	}

	// Store session data
	if err := s.client.Set(ctx, s.sessionKey(session.ID), data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store session: %w", err)
	}

	// Add to user's session set
	if err := s.client.SAdd(ctx, s.userSessionsKey(session.UserID), session.ID).Err(); err != nil {
		return fmt.Errorf("failed to add session to user set: %w", err)
	}

	return nil
}

// Get retrieves a session by ID
func (s *RedisSessionStore) Get(ctx context.Context, id string) (*Session, error) {
	data, err := s.client.Get(ctx, s.sessionKey(id)).Bytes()
	if err == redis.Nil {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	return &session, nil
}

// Update updates an existing session
func (s *RedisSessionStore) Update(ctx context.Context, session *Session) error {
	// Check if session exists
	exists, err := s.client.Exists(ctx, s.sessionKey(session.ID)).Result()
	if err != nil {
		return fmt.Errorf("failed to check session existence: %w", err)
	}
	if exists == 0 {
		return ErrSessionNotFound
	}

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	// Calculate remaining TTL
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		// Session expired, delete it
		return s.Delete(ctx, session.ID)
	}

	if err := s.client.Set(ctx, s.sessionKey(session.ID), data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// Delete removes a session
func (s *RedisSessionStore) Delete(ctx context.Context, id string) error {
	// Get session to find user ID
	session, err := s.Get(ctx, id)
	if err != nil && err != ErrSessionNotFound {
		return err
	}

	// Delete session
	if err := s.client.Del(ctx, s.sessionKey(id)).Err(); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	// Remove from user's session set if we found the session
	if session != nil {
		s.client.SRem(ctx, s.userSessionsKey(session.UserID), id)
	}

	return nil
}

// GetByUserID returns all sessions for a user
func (s *RedisSessionStore) GetByUserID(ctx context.Context, userID string) ([]*Session, error) {
	// Get all session IDs for this user
	sessionIDs, err := s.client.SMembers(ctx, s.userSessionsKey(userID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get user sessions: %w", err)
	}

	sessions := make([]*Session, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		session, err := s.Get(ctx, id)
		if err == ErrSessionNotFound {
			// Session expired, remove from set
			s.client.SRem(ctx, s.userSessionsKey(userID), id)
			continue
		}
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// DeleteByUserID removes all sessions for a user
func (s *RedisSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	// Get all session IDs for this user
	sessionIDs, err := s.client.SMembers(ctx, s.userSessionsKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("failed to get user sessions: %w", err)
	}

	// Delete all sessions
	for _, id := range sessionIDs {
		s.client.Del(ctx, s.sessionKey(id))
	}

	// Delete the user's session set
	if err := s.client.Del(ctx, s.userSessionsKey(userID)).Err(); err != nil {
		return fmt.Errorf("failed to delete user session set: %w", err)
	}

	return nil
}

// CountByUserID returns the number of active sessions for a user
func (s *RedisSessionStore) CountByUserID(ctx context.Context, userID string) (int, error) {
	count, err := s.client.SCard(ctx, s.userSessionsKey(userID)).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to count user sessions: %w", err)
	}
	return int(count), nil
}

// RedisTokenStore implements TokenStore using Redis
type RedisTokenStore struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisTokenStore creates a new Redis token store
func NewRedisTokenStore(client *redis.Client, keyPrefix string) *RedisTokenStore {
	if keyPrefix == "" {
		keyPrefix = "mycel:auth:token:"
	}
	return &RedisTokenStore{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// refreshTokenKey returns the Redis key for a refresh token
func (s *RedisTokenStore) refreshTokenKey(token string) string {
	return s.keyPrefix + "refresh:" + token
}

// userTokensKey returns the Redis key for user's token list
func (s *RedisTokenStore) userTokensKey(userID string) string {
	return s.keyPrefix + "user:" + userID
}

// blacklistKey returns the Redis key for blacklisted tokens
func (s *RedisTokenStore) blacklistKey(jti string) string {
	return s.keyPrefix + "blacklist:" + jti
}

// StoreRefreshToken stores a refresh token with TTL
func (s *RedisTokenStore) StoreRefreshToken(ctx context.Context, token, userID string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return fmt.Errorf("token already expired")
	}

	data := map[string]string{
		"user_id":    userID,
		"expires_at": expiresAt.Format(time.RFC3339),
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal token data: %w", err)
	}

	// Store token
	if err := s.client.Set(ctx, s.refreshTokenKey(token), jsonData, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Add to user's token set
	if err := s.client.SAdd(ctx, s.userTokensKey(userID), token).Err(); err != nil {
		return fmt.Errorf("failed to add token to user set: %w", err)
	}

	return nil
}

// ValidateRefreshToken checks if a refresh token is valid
func (s *RedisTokenStore) ValidateRefreshToken(ctx context.Context, token string) (string, error) {
	data, err := s.client.Get(ctx, s.refreshTokenKey(token)).Bytes()
	if err == redis.Nil {
		return "", ErrTokenExpired
	}
	if err != nil {
		return "", fmt.Errorf("failed to get refresh token: %w", err)
	}

	var tokenData map[string]string
	if err := json.Unmarshal(data, &tokenData); err != nil {
		return "", fmt.Errorf("failed to unmarshal token data: %w", err)
	}

	return tokenData["user_id"], nil
}

// RevokeRefreshToken removes a refresh token
func (s *RedisTokenStore) RevokeRefreshToken(ctx context.Context, token string) error {
	// Get token data to find user ID
	data, err := s.client.Get(ctx, s.refreshTokenKey(token)).Bytes()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get refresh token: %w", err)
	}

	// Delete the token
	if err := s.client.Del(ctx, s.refreshTokenKey(token)).Err(); err != nil {
		return fmt.Errorf("failed to delete refresh token: %w", err)
	}

	// Remove from user's token set if we found the token
	if data != nil {
		var tokenData map[string]string
		if json.Unmarshal(data, &tokenData) == nil {
			if userID, ok := tokenData["user_id"]; ok {
				s.client.SRem(ctx, s.userTokensKey(userID), token)
			}
		}
	}

	return nil
}

// RevokeAllUserTokens removes all refresh tokens for a user
func (s *RedisTokenStore) RevokeAllUserTokens(ctx context.Context, userID string) error {
	// Get all tokens for this user
	tokens, err := s.client.SMembers(ctx, s.userTokensKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("failed to get user tokens: %w", err)
	}

	// Delete all tokens
	for _, token := range tokens {
		s.client.Del(ctx, s.refreshTokenKey(token))
	}

	// Delete the user's token set
	if err := s.client.Del(ctx, s.userTokensKey(userID)).Err(); err != nil {
		return fmt.Errorf("failed to delete user token set: %w", err)
	}

	return nil
}

// BlacklistToken adds a token JTI to the blacklist
func (s *RedisTokenStore) BlacklistToken(ctx context.Context, jti string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		// Token already expired, no need to blacklist
		return nil
	}

	if err := s.client.Set(ctx, s.blacklistKey(jti), "1", ttl).Err(); err != nil {
		return fmt.Errorf("failed to blacklist token: %w", err)
	}

	return nil
}

// IsBlacklisted checks if a token JTI is blacklisted
func (s *RedisTokenStore) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	exists, err := s.client.Exists(ctx, s.blacklistKey(jti)).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check blacklist: %w", err)
	}
	return exists > 0, nil
}

// RedisBruteForceStore implements BruteForceStore using Redis
type RedisBruteForceStore struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisBruteForceStore creates a new Redis brute force store
func NewRedisBruteForceStore(client *redis.Client, keyPrefix string) *RedisBruteForceStore {
	if keyPrefix == "" {
		keyPrefix = "mycel:auth:bruteforce:"
	}
	return &RedisBruteForceStore{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// attemptsKey returns the Redis key for attempt tracking
func (s *RedisBruteForceStore) attemptsKey(key string) string {
	return s.keyPrefix + "attempts:" + key
}

// lockoutKey returns the Redis key for lockout tracking
func (s *RedisBruteForceStore) lockoutKey(key string) string {
	return s.keyPrefix + "lockout:" + key
}

// delayKey returns the Redis key for progressive delay tracking
func (s *RedisBruteForceStore) delayKey(key string) string {
	return s.keyPrefix + "delay:" + key
}

// Increment increments the failure count for a key (implements BruteForceStore)
func (s *RedisBruteForceStore) Increment(ctx context.Context, key string, window time.Duration) (int, error) {
	attKey := s.attemptsKey(key)

	// Increment attempt counter
	count, err := s.client.Incr(ctx, attKey).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment attempts: %w", err)
	}

	// Set expiration on first attempt
	if count == 1 {
		s.client.Expire(ctx, attKey, window)
	}

	return int(count), nil
}

// Get returns the current failure count for a key (implements BruteForceStore)
func (s *RedisBruteForceStore) Get(ctx context.Context, key string) (int, error) {
	count, err := s.client.Get(ctx, s.attemptsKey(key)).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get attempts: %w", err)
	}
	return count, nil
}

// GetAttempts is an alias for Get (implements BruteForceStore)
func (s *RedisBruteForceStore) GetAttempts(ctx context.Context, key string) (int, error) {
	return s.Get(ctx, key)
}

// Reset resets the failure count for a key (implements BruteForceStore)
func (s *RedisBruteForceStore) Reset(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, s.attemptsKey(key)).Err(); err != nil {
		return fmt.Errorf("failed to clear attempts: %w", err)
	}
	// Also clear any progressive delay
	s.client.Del(ctx, s.delayKey(key))
	return nil
}

// Lock locks a key for a duration (implements BruteForceStore)
func (s *RedisBruteForceStore) Lock(ctx context.Context, key string, duration time.Duration) error {
	if err := s.client.Set(ctx, s.lockoutKey(key), "1", duration).Err(); err != nil {
		return fmt.Errorf("failed to set lockout: %w", err)
	}
	return nil
}

// IsLocked checks if a key is locked (implements BruteForceStore)
func (s *RedisBruteForceStore) IsLocked(ctx context.Context, key string) (bool, time.Time, error) {
	ttl, err := s.client.TTL(ctx, s.lockoutKey(key)).Result()
	if err != nil {
		return false, time.Time{}, fmt.Errorf("failed to check lockout: %w", err)
	}

	if ttl < 0 {
		// Key doesn't exist or has no TTL
		return false, time.Time{}, nil
	}

	lockUntil := time.Now().Add(ttl)
	return true, lockUntil, nil
}

// GetDelay returns the current progressive delay for a key (implements BruteForceStore)
func (s *RedisBruteForceStore) GetDelay(ctx context.Context, key string) (time.Duration, error) {
	delayMs, err := s.client.Get(ctx, s.delayKey(key)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get delay: %w", err)
	}
	return time.Duration(delayMs) * time.Millisecond, nil
}

// SetDelay sets the progressive delay for a key (implements BruteForceStore)
func (s *RedisBruteForceStore) SetDelay(ctx context.Context, key string, delay time.Duration, window time.Duration) error {
	delayMs := delay.Milliseconds()
	if err := s.client.Set(ctx, s.delayKey(key), strconv.FormatInt(delayMs, 10), window).Err(); err != nil {
		return fmt.Errorf("failed to set delay: %w", err)
	}
	return nil
}

// RedisReplayProtectionStore implements replay protection using Redis
type RedisReplayProtectionStore struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisReplayProtectionStore creates a new Redis replay protection store
func NewRedisReplayProtectionStore(client *redis.Client, keyPrefix string) *RedisReplayProtectionStore {
	if keyPrefix == "" {
		keyPrefix = "mycel:auth:replay:"
	}
	return &RedisReplayProtectionStore{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// MarkTokenUsed marks a token JTI as used (for one-time use tokens)
func (s *RedisReplayProtectionStore) MarkTokenUsed(ctx context.Context, jti string, window time.Duration) error {
	if err := s.client.Set(ctx, s.keyPrefix+jti, "1", window).Err(); err != nil {
		return fmt.Errorf("failed to mark token used: %w", err)
	}
	return nil
}

// IsTokenUsed checks if a token JTI has been used
func (s *RedisReplayProtectionStore) IsTokenUsed(ctx context.Context, jti string) (bool, error) {
	exists, err := s.client.Exists(ctx, s.keyPrefix+jti).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check token usage: %w", err)
	}
	return exists > 0, nil
}

// RedisClient wraps redis.Client creation
func NewRedisClient(addr, password string, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}
