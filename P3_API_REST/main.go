package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// POLÍTICA (para documentar):
// - Token JWT HS256, secreto aleatorio de 32 bytes, caduca a los 15 minutos.
// - Contraseñas con bcrypt.
// - Login: 5 fallos seguidos de un usuario => bloqueado 15 min (429).
// - Signup: máximo 5 intentos por IP cada 10 min (429).
// - Usuarios en memoria. Pokédex en ficheros data/<usuario>/<dex>.json.

var users = map[string][]byte{}

var intentosReg = map[string]int{}     // ip -> intentos
var inicioReg = map[string]time.Time{} // ip -> cuándo empezó la cuenta

var fallosLogin = map[string]int{}          // usuario -> fallos seguidos
var bloqueadoHasta = map[string]time.Time{} // usuario -> hasta cuándo

const (
	duracionToken = 15 * time.Minute
	dirDatos      = "data"
)

var jwtRand = make([]byte, 32)

// Hash falso, se usa cuando el usuario no existe, para tardar lo mismo en responder
var hashFalso, _ = bcrypt.GenerateFromPassword([]byte("asdfg"), bcrypt.DefaultCost)

var tipos = []string{"normal", "fire", "water", "grass", "electric", "ice",
	"fighting", "poison", "ground", "flying", "psychic", "bug",
	"rock", "ghost", "dragon", "dark", "steel", "fairy"}

// Structs

type versionResponse struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

type userRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type resumen struct {
	Dex  int    `json:"dex"`
	Name string `json:"name"`
}

type ataque struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Level int    `json:"level"`
}

type pokemon struct {
	Dex     int      `json:"dex"`
	Name    string   `json:"name"`
	Types   []string `json:"types"`
	Attacks []ataque `json:"attacks"`
}

//FUNCIONES AUXILIARES

func tipoValido(t string) bool {
	for _, v := range tipos {
		if t == v {
			return true
		}
	}
	return false
}

func validarNombrePokemon(nombre string) bool {
	if len(nombre) < 2 || len(nombre) > 20 {
		return false
	}
	for _, ch := range nombre {
		val := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			ch == ' ' || ch == '-' || ch == '\'' || ch == '.'
		if !val {
			return false
		}
	}
	return true
}

func validarDex(s string) bool {
	if len(s) < 1 || len(s) > 4 || s[0] == '0' {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return s <= "1025" || len(s) < 4
}

func pokemonValido(p pokemon, dexURL int) bool {
	// dex 1-1025
	if p.Dex < 1 || p.Dex > 1025 || p.Dex != dexURL {
		return false
	}

	// name
	if !validarNombrePokemon(p.Name) {
		return false
	}

	// types 1 o 2 que sea nde la lista y no repetidos
	if len(p.Types) < 1 || len(p.Types) > 2 {
		return false
	}
	for _, t := range p.Types {
		if !tipoValido(t) {
			return false
		}
	}
	if len(p.Types) == 2 && p.Types[0] == p.Types[1] {
		return false
	}

	// attacks 1-20
	if len(p.Attacks) < 1 || len(p.Attacks) > 20 {
		return false
	}
	nombres := map[string]bool{}
	for _, a := range p.Attacks {
		// nombre del ataque 2 a 30 caracteres
		if len(a.Name) < 2 || len(a.Name) > 30 {
			return false
		}
		// tipo del ataque del enumerado
		if !tipoValido(a.Type) {
			return false
		}
		// level 1-100
		if a.Level < 1 || a.Level > 100 {
			return false
		}
		// dos ataques con el mismo nombre no
		if nombres[a.Name] {
			return false
		}
		nombres[a.Name] = true
	}
	return true
}

func validarUsername(username string) bool {
	if len(username) < 3 || len(username) > 20 {
		return false
	}
	for _, ch := range username {
		val := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_'
		if !val {
			return false
		}
	}
	return true
}

func authRequired(c *gin.Context) {
	cabecera := c.GetHeader("Authorization")

	if !strings.HasPrefix(cabecera, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	tokenTexto := strings.TrimPrefix(cabecera, "Bearer ")

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenTexto, claims, func(t *jwt.Token) (any, error) {
		return jwtRand, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())

	if err != nil || !token.Valid || claims.Subject == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	c.Set("username", claims.Subject)
}

func mismoUser(c *gin.Context) {
	userToken := c.GetString("username")
	userURL := c.Param("user")
	if userToken != userURL {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
	}

	if _, exists := users[userURL]; !exists {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	}
}

// funciones GET/POST

func getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, versionResponse{
		Service: "secure-pokeapi",
		Version: "1.0.0",
	})
}

func postLogin(c *gin.Context) {
	// Límite del cuerpo a 10 KB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10240)

	// 400 bad request
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	// 429 usuario bloqueado
	if time.Now().Before(bloqueadoHasta[req.Username]) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
		return
	}

	hash, exists := users[req.Username]

	// Usamos un hash falso para que la API tarde lo mismo en responder, esto lo debería hacer más seguro
	if !exists {
		hash = hashFalso
	}

	err := bcrypt.CompareHashAndPassword(hash, []byte(req.Password))

	if !exists || err != nil {
		fallosLogin[req.Username]++
		if fallosLogin[req.Username] >= 5 {
			bloqueadoHasta[req.Username] = time.Now().Add(15 * time.Minute)
			fallosLogin[req.Username] = 0
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	fallosLogin[req.Username] = 0
	ahora := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   req.Username,
		IssuedAt:  jwt.NewNumericDate(ahora),
		ExpiresAt: jwt.NewNumericDate(ahora.Add(duracionToken)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtRand)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})

}

func postSignup(c *gin.Context) {
	// 429 too many requeests
	ip := c.ClientIP()
	if time.Since(inicioReg[ip]) > 10*time.Minute {
		intentosReg[ip] = 0
		inicioReg[ip] = time.Now()
	}

	intentosReg[ip]++
	if intentosReg[ip] > 5 {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
		return
	}

	// Límite del cuerpo a 10 KB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10240)

	// 400 bad request
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	// 422 unprocessable entity
	if !validarUsername(req.Username) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid username"})
		return
	}

	// 409 conflict
	if _, exists := users[req.Username]; exists {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}

	// Hash con sal (bcrypt genera la sal y la guarda dentro del hash)
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		// bcrypt falla si la contraseña supera 72 bytes, devolvemos error 400
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	// 201 created

	users[req.Username] = hash
	c.JSON(http.StatusCreated, gin.H{"username": req.Username})
}

func getPokedex(c *gin.Context) {
	user := c.Param("user")
	lista := []resumen{}

	carpeta := filepath.Join(dirDatos, user)
	ficheros, err := os.ReadDir(carpeta)
	if err != nil {
		c.JSON(http.StatusOK, lista)
		return
	}

	for _, f := range ficheros {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		datos, err := os.ReadFile(filepath.Join(carpeta, f.Name()))
		if err != nil {
			continue
		}
		var salida resumen
		if json.Unmarshal(datos, &salida) == nil {
			lista = append(lista, salida)
		}
	}

	c.JSON(http.StatusOK, lista)
}

func getPokemon(c *gin.Context) {
	dex := c.Param("dex")
	if !validarDex(dex) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	ruta := filepath.Join(dirDatos, c.Param("user"), dex+".json")
	datos, err := os.ReadFile(ruta)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	c.Data(http.StatusOK, "application/json", datos)
}

func putPokemon(c *gin.Context) {
	dex := c.Param("dex")
	if !validarDex(dex) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10240)

	var pokemon pokemon
	if err := c.ShouldBindJSON(&pokemon); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	// 422
	numero, _ := strconv.Atoi(dex)
	if !pokemonValido(pokemon, numero) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid document"})
		return
	}

	carpeta := filepath.Join(dirDatos, c.Param("user"))
	ruta := filepath.Join(carpeta, dex+".json")

	_, errStat := os.Stat(ruta)
	exists := errStat == nil

	// para que sea más fácil de leer usamos MarshalIndent en lugar de Marshal, así tenemos identaciones
	datos, err := json.MarshalIndent(pokemon, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := os.MkdirAll(carpeta, 0750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := os.WriteFile(ruta, datos, 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	if exists {
		c.JSON(http.StatusOK, pokemon)
	} else {
		c.JSON(http.StatusCreated, pokemon)
	}
}

func deletePokemon(c *gin.Context) {
	dex := c.Param("dex")
	if !validarDex(dex) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	ruta := filepath.Join(dirDatos, c.Param("user"), dex+".json")

	if _, err := os.Stat(ruta); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	if err := os.Remove(ruta); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Status(http.StatusNoContent) 
}

func main() {
	// para rechazar campos adicionales en todos los endpoints
	binding.EnableDecoderDisallowUnknownFields = true

	// para limpiar los datos al iniciar el programa y así poder hacer signups y todos los pasos desde 0
	os.RemoveAll(dirDatos)
 
	rand.Read(jwtRand)

	router := gin.Default()

	// sin autenticación
	router.GET("/version", getVersion)
	router.POST("/login", postLogin)
	router.POST("/signup", postSignup)
	// con autenticación
	router.GET("/user/:user/pokedex", authRequired, mismoUser, getPokedex)
	router.GET("/user/:user/pokedex/:dex", authRequired, mismoUser, getPokemon)
	router.PUT("/user/:user/pokedex/:dex", authRequired, mismoUser, putPokemon)
	router.DELETE("/user/:user/pokedex/:dex", authRequired, mismoUser, deletePokemon)

	router.Run("localhost:8080")
}
