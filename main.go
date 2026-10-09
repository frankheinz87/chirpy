package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/frankheinz87/chirpy/internal/auth"
	"github.com/frankheinz87/chirpy/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits  atomic.Int32
	databaseQueries *database.Queries
	platform        string
	tokenSecret     string
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	pt := os.Getenv("PLATFORM")
	secret := os.Getenv("SECRET")
	db, err := sql.Open("postgres", dbURL)

	if err != nil {
		log.Fatalf("Error opening database connection: %v", err)
	}

	dbQueries := database.New(db)

	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	fs := http.FileServer(http.Dir("."))
	cfg := &apiConfig{
		databaseQueries: dbQueries,
		platform:        pt,
		tokenSecret:     secret,
	}

	mux.Handle("/app/", cfg.middlewareMetricsInc(http.StripPrefix("/app", fs)))
	mux.HandleFunc("GET /api/healthz", myhandler)
	mux.HandleFunc("GET /admin/metrics", cfg.metricshandler)
	mux.HandleFunc("GET /api/chirps", cfg.getchirpshandler)
	mux.HandleFunc("GET /api/chirps/{chirpID}", cfg.getchirphandler)
	mux.HandleFunc("POST /admin/reset", cfg.resethandler)
	mux.HandleFunc("POST /api/chirps", cfg.postchirphandler)
	mux.HandleFunc("POST /api/users", cfg.userhandler)
	mux.HandleFunc("POST /api/login", cfg.loginhandler)

	log.Fatal(server.ListenAndServe())

}

func myhandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (cfg *apiConfig) metricshandler(w http.ResponseWriter, r *http.Request) {
	count := cfg.fileserverHits.Load()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// fmt.Fprintf(w, "Hits: %d", count)
	html := fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, count)
	w.Write([]byte(html))
}

func (cfg *apiConfig) resethandler(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	cfg.fileserverHits.Store(0)
	err := cfg.databaseQueries.DeleteUsers(r.Context())

	if err != nil {
		log.Printf("Error deleting users: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (cfg *apiConfig) postchirphandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}

	params := parameters{}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't decode parameters")
		return
	}

	if len(params.Body) > 140 {
		respondWithError(w, http.StatusBadRequest, "Chirp is too long")
		return
	}

	tokenString, err := auth.GetBearerToken(r.Header)

	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	authUserID, err := auth.ValidateJWT(tokenString, cfg.tokenSecret)

	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	chirp, err := cfg.databaseQueries.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   cleanProfanity(params.Body),
		UserID: authUserID,
	})

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create chirp")
		return
	}

	respondWithJSON(w, http.StatusCreated, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

func (cfg *apiConfig) getchirpshandler(w http.ResponseWriter, r *http.Request) {

	dbchirps, err := cfg.databaseQueries.GetChirps(r.Context())

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get chirps")
		return
	}

	chirpslice := make([]Chirp, len(dbchirps))

	for i, dbchirp := range dbchirps {
		chirpslice[i] = Chirp{
			ID:        dbchirp.ID,
			CreatedAt: dbchirp.CreatedAt,
			UpdatedAt: dbchirp.UpdatedAt,
			Body:      dbchirp.Body,
			UserID:    dbchirp.UserID,
		}
	}

	respondWithJSON(w, http.StatusOK, chirpslice)
}

func (cfg *apiConfig) getchirphandler(w http.ResponseWriter, r *http.Request) {

	id, err := uuid.Parse(r.PathValue("chirpID"))

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "id not valid")
		return
	}

	chirp, err := cfg.databaseQueries.GetChirpByID(r.Context(), id)

	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't find chirp")
		return
	}

	respondWithJSON(w, http.StatusOK, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

func cleanProfanity(body string) string {
	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}

	words := strings.Split(body, " ")
	for i, word := range words {
		if _, ok := badWords[strings.ToLower(word)]; ok {
			words[i] = "****"
		}
	}
	return strings.Join(words, " ")
}

func (cfg *apiConfig) userhandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	params := parameters{}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't decode parameters")
		return
	}

	nullableEmail := sql.NullString{
		String: params.Email,
		Valid:  params.Email != "", // Becomes false if the string is empty
	}

	hashedPW, err := auth.HashPassword(params.Password)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't hash password")
		return
	}

	user, err := cfg.databaseQueries.CreateUser(r.Context(), database.CreateUserParams{
		Email:          nullableEmail,
		HashedPassword: hashedPW,
	})

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create user")
		return
	}

	respBody := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email.String,
	}
	statusCode := http.StatusCreated

	respondWithJSON(w, statusCode, respBody)

}

func (cfg *apiConfig) loginhandler(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Password         string `json:"password"`
		Email            string `json:"email"`
		EXpiresInSeconds *int   `json:"expires_in_seconds"`
	}

	params := parameters{}

	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't decode parameters")
		return
	}

	expiresIn := 3600

	if params.EXpiresInSeconds != nil && *params.EXpiresInSeconds < 3600 {
		expiresIn = *params.EXpiresInSeconds
	}

	duration := time.Duration(expiresIn) * time.Second

	nullableEmail := sql.NullString{
		String: params.Email,
		Valid:  params.Email != "", // Becomes false if the string is empty
	}

	user, err := cfg.databaseQueries.GetUserByEmail(r.Context(), nullableEmail)

	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	validPassword, err := auth.CheckPasswordHash(params.Password, user.HashedPassword)

	if err != nil || !validPassword {
		respondWithError(w, http.StatusUnauthorized, "incorrect email or password")
		return
	}

	token, err := auth.MakeJWT(user.ID, cfg.tokenSecret, duration)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating token")
		return
	}

	respBody := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email.String,
		Token:     token,
	}

	respondWithJSON(w, http.StatusOK, respBody)
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(dat)
}

func respondWithError(w http.ResponseWriter, code int, msg string) {
	respondWithJSON(w, code, errorResponse{Error: msg})
}
