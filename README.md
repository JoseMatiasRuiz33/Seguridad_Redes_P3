# Seguridad en Redes P3: PokéAPI segura, cómo ejecutarla y probarla con Thunder Client

API REST en Go (Gin) con autenticación JWT, contraseñas con bcrypt y control de acceso por usuario.
Esta guía explica cómo arrancarla y cómo pasar las **11 pruebas de aceptación** del enunciado usando la extensión **Thunder Client** de Visual Studio Code.

---

## 1. Ejecutar la API

Requisitos: Go instalado.

```bash
go mod tidy
go run .
```

El servidor escucha en `http://localhost:8080` (HTTP, sin TLS).

> Al arrancar se borra la carpeta `data/`, porque los usuarios viven solo en memoria. Si reinicias el servidor tendrás que volver a hacer signup y login.

---

## 2. Preparar Thunder Client

1. Instala la extensión **Thunder Client** en VS Code (icono del rayo en la barra lateral).
2. Pestaña **Collections** → **New Collection** → llámala `pokeapi`. Crea ahí todas las peticiones.
3. Pestaña **Env** → **New Environment** → `local`, y márcalo como activo (estrella). Añade estas variables:

| Variable | Valor inicial |
|---|---|
| `base` | `http://localhost:8080` |
| `token_ash` | *(vacío)* |
| `token_misty` | *(vacío)* |

A partir de ahora las URLs se escriben como `{{base}}/version`.

### Guardar el token automáticamente

En las peticiones de login, abre la pestaña **Vars** → **Set Env Variable** y añade:

| Campo | Valor |
|---|---|
| Nombre | `token_ash` (o `token_misty`) |
| Fuente | `json.token` |

Así no tienes que copiar el token a mano.

### Enviar el token en las peticiones autenticadas

En la pestaña **Auth** → **Bearer** y escribe `{{token_ash}}` en el campo *Token*.
(Thunder Client añade `Bearer ` solo)

---

## 3. Antes de nada, hay que cambiar dos ajustes para las pruebas

### 3.1 Límite de signup

El enunciado pide limitar el registro. Con el límite actual (**5 intentos por IP cada 10 minutos**), las pruebas 2, 3 y 4 hacen unos 10 signups seguidos si probamos todo y a partir del sexto recibirás **429**.

- **Para probar:** sube temporalmente el límite en `postSignup` (por ejemplo de `5` a `50`), reinicia y haz las pruebas 1 a 10. Para la prueba 11 vuelve a poner `5`.

### 3.2 Orden

El bloqueo de login dura **15 minutos**. Haz la **prueba 11 la última**. Si necesitas repetir todo, reinicia el servidor.

---

## 4. Pruebas de aceptación

Todas las peticiones llevan la cabecera `Content-Type: application/json` cuando tienen cuerpo (Thunder Client la pone solo al elegir **Body → JSON**).

### Prueba 1: `GET /version` sin token

- **GET** `{{base}}/version`
- Esperado: **200** con `{"service":"secure-pokeapi","version":"1.0.0"}`

### Prueba 2: crear `ash` y `misty`

**POST** `{{base}}/signup`, Body → JSON:

```json
{ "username": "ash", "password": "pikachu123" }
```

```json
{ "username": "misty", "password": "staryu123" }
```

- Esperado: **201** con `{"username":"ash"}` (y `misty`). Sin token, contraseña ni hash.

### Prueba 3: usuario duplicado

- Repite el signup de `ash`.
- Esperado: **409**.

### Prueba 4: usernames inválidos y JSON roto

**POST** `{{base}}/signup` con cada uno de estos cuerpos:

| Cuerpo | Esperado |
|---|---|
| `{ "username": "../x", "password": "abc" }` | **422** |
| `{ "username": "Ash", "password": "abc" }` | **422** |
| `{ "username": "a", "password": "abc" }` | **422** |
| `{ "username": "abcdefghijklmnopqrstuvwxyz", "password": "abc" }` (más de 20) | **422** |
| `{ "username": "mi usuario", "password": "abc" }` | **422** |
| `{ "username": "` *(JSON roto, déjalo sin cerrar)* | **400** |
| `{ "username": "brock" }` *(falta password)* | **400** |

### Prueba 5: login

**POST** `{{base}}/login`

Credenciales correctas (guarda el token con **Vars**, ver sección 2):

```json
{ "username": "ash", "password": "pikachu123" }
```

- Esperado: **200** con `{"token":"eyJ..."}`.

Haz lo mismo con `misty` y guarda `token_misty`.

Credenciales incorrectas, dos casos:

```json
{ "username": "ash", "password": "mala" }
```

```json
{ "username": "noexiste", "password": "mala" }
```

- Esperado: **401** en ambos, con **el mismo mensaje** (`{"error":"invalid credentials"}`).

> Cada fallo cuenta para el bloqueo. Con 2 fallos de `ash` no hay problema, pero no pases de 4.

### Prueba 6: sin token, token basura o caducado

Usa **GET** `{{base}}/user/ash/pokedex` y comprueba que **todas** dan **401**:

| Caso | Cómo hacerlo |
|---|---|
| Sin token | Pestaña Auth en *None* |
| Token basura | Bearer con `abc.def.ghi` |
| Token manipulado | Bearer con tu token y una letra cambiada al final |
| Token caducado | Ver más abajo |

**Token caducado.** Para no esperar 15 minutos, cambia temporalmente en el código:

```go
duracionToken = 5 * time.Second
```

Reinicia, haz signup y login, espera unos 6 segundos y usa ese token: debe dar **401**. Después vuelve a poner `15 * time.Minute` y reinicia.

Repite también el caso "sin token" con los otros tres endpoints de datos (`GET .../pokedex/25`, `PUT`, `DELETE`).

### Prueba 7: flujo normal con el token de `ash`

Auth Bearer: `{{token_ash}}`.

**7a. PUT** `{{base}}/user/ash/pokedex/25`, Body → JSON:

```json
{
  "dex": 25,
  "name": "Pikachu",
  "types": ["electric"],
  "attacks": [
    { "name": "Thunder Shock", "type": "electric", "level": 1 },
    { "name": "Thunder Wave", "type": "electric", "level": 10 }
  ]
}
```

- Primera vez: **201**. Enviándolo otra vez idéntico: **200**.

**7b. GET** `{{base}}/user/ash/pokedex/25`
- Esperado: **200** con el documento completo.

**7c. GET** `{{base}}/user/ash/pokedex`
- Esperado: **200** con `[{"dex":25,"name":"Pikachu"}]`.

**7d. DELETE** `{{base}}/user/ash/pokedex/25`
- Esperado: **204**. Repetirlo: **404**. (Vuelve a hacer el PUT si quieres seguir probando con esa entrada.)

### Prueba 8: acceso cruzado

Con el token de `ash` (`{{token_ash}}`), las cuatro operaciones sobre `misty` deben dar **403**:

| Método | URL |
|---|---|
| GET | `{{base}}/user/misty/pokedex` |
| GET | `{{base}}/user/misty/pokedex/25` |
| PUT | `{{base}}/user/misty/pokedex/25` (con el JSON del ejemplo) |
| DELETE | `{{base}}/user/misty/pokedex/25` |

Comprueba después, con el token de `misty`, que `GET {{base}}/user/misty/pokedex` devuelve `[]`, es decir, que el PUT de ash no escribió nada.

### Prueba 9: validación del documento

**PUT** `{{base}}/user/ash/pokedex/25` con token de `ash`. Parte del documento del ejemplo y cambia solo lo indicado:

| Caso | Cambio | Esperado |
|---|---|---|
| Tipos repetidos | `"types": ["fire","fire"]` | **422** |
| Ataques duplicados | dos ataques con `"name": "Thunder Shock"` | **422** |
| Campo extra | añadir `"hp": 35` | **400** o **422** |
| Campo extra en un ataque/pokemon | añadir `"power": 40` dentro de un ataque | **400** |
| `dex` distinto del de la URL | `"dex": 26` con URL `/25` | **422** |
| Tipo inválido | `"types": ["fuego"]` | **422** |
| Nivel fuera de rango | `"level": 500` | **422** |
| Sin tipos | `"types": []` | **422** |
| Sin ataques | `"attacks": []` | **422** |
| Falta un campo | quitar `"name"` | **422** |
| Nombre inválido | `"name": "Pika123"` | **422** |
| JSON roto | dejar el JSON sin cerrar | **400** |
| Cuerpo vacío | Body vacío | **400** |
| Cuerpo de más de 10 KB | ver más abajo | **400** |


**Cuerpo de más de 10 KB.** Es difícil pegarlo en Thunder Client. Recomiendo lanzarlo desde terminal (sustituye `$TOKEN` por el token de ash):

```bash
curl -i -X PUT http://localhost:8080/user/ash/pokedex/25 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"dex\":25,\"name\":\"$(head -c 11000 /dev/zero | tr '\0' 'a')\"}"
```

Esperado: **400**.

### Prueba 10: entrada hostil

Con el token de `ash`:

| Petición | Esperado |
|---|---|
| `GET {{base}}/user/..%2F..%2Fetc/pokedex/25` | Nunca contenido del sistema. Sin token da **401**; con el token de ash da **403** (o 404 si el router no casa la ruta) |
| `GET {{base}}/user/ash/pokedex/abc` | **400** |
| `GET {{base}}/user/ash/pokedex/-1` | **400** |
| `GET {{base}}/user/ash/pokedex/+5` | **400** |
| `GET {{base}}/user/ash/pokedex/007` | **400** |
| `GET {{base}}/user/ash/pokedex/2.5` | **400** |
| `GET {{base}}/user/ash/pokedex/0` | **400** |
| `GET {{base}}/user/ash/pokedex/2000` | **400** |
| `GET {{base}}/user/ash/pokedex/..%2F..%2Fetc%2Fpasswd` | **400** o 404, no da trazas del contenido del sistema |

> Thunder Client puede re-codificar `%2F` al enviar. Si dudas de lo que llega realmente, repite esa petición con curl y `--path-as-is`:
>
> ```bash
> curl -i --path-as-is "http://localhost:8080/user/..%2F..%2Fetc/pokedex/25" -H "Authorization: Bearer $TOKEN"
> ```
>
> Lo importante es que en ningún caso aparezca contenido de ficheros del sistema ni mensajes de error internos (rutas, trazas de Go).

### Prueba 11: límite de intentos (dejarla para el final)

**Login.** **POST** `{{base}}/login` con una contraseña incorrecta, 6 veces seguidas:

```json
{ "username": "misty", "password": "incorrecta" }
```

- Intentos 1 a 5: **401**. A partir del siguiente: **429**.
- Con la contraseña correcta, mientras dure el bloqueo (15 min), también da **429**.

**Signup.** Vuelve a dejar el límite en `5`, reinicia y envía 6 signups seguidos (válidos o no):

- Los 5 primeros responden con su código normal (201, 409, 422...).
- El sexto: **429**.

Thunder Client permite lanzar una petición varias veces seguidas con **Send** repetido, o puedes usar la opción **Run collection** para ejecutar varias.

---

## 5. Tabla resumen

| # | Prueba | Código esperado |
|---|---|---|
| 1 | GET /version | 200 |
| 2 | Signup ash y misty | 201 |
| 3 | Signup duplicado | 409 |
| 4 | Username inválido / JSON roto o campos que faltan | 422 / 400 |
| 5 | Login correcto / incorrecto | 200 / 401 (mismo mensaje) |
| 6 | Sin token, basura o caducado | 401 |
| 7 | PUT (x2), GET, listado con ash | 201 y 200, 200, 200 |
| 8 | Operaciones de ash sobre misty | 403 (las cuatro) |
| 9 | Documentos inválidos | 400 o 422 |
| 10 | Entrada hostil | Sin contenido del sistema; 400 / 403 |
| 11 | Ráfagas de login y signup | 429 |

---

## 6. Fallos comunes

- **401 inesperado:** el token caduca a los 15 minutos, o se ha reiniciado el servidor (cambia la clave de firma). Repite el login.
- **403 con tu propio usuario:** al reiniciar se pierden los usuarios. Hay que hacer otro signup.
- **429 en signup:** has superado el límite de intentos. Reinicia el servidor o sube el límite para las pruebas.
- **No veo la respuesta completa:** en Thunder Client, pestaña **Response** → **Headers** para ver el código y la cabecera `Content-Type`.
