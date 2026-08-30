module github.com/activialtd/gomarketi.com-backend/services/admin-api/scripts/jwt-helper

go 1.22.0

require github.com/activialtd/gomarketi.com-backend/shared/pkg v0.0.0-00010101000000-000000000000

require (
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace github.com/activialtd/gomarketi.com-backend/shared/pkg => ../../../../shared/pkg
