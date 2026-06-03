# pgxmigrate

pgx/pgxpool ile calisan, `UP/DOWN` bolumlu SQL migration paketi ve CLI araci.

## Kurulum

Paket olarak kullanmak icin:

```bash
go get github.com/mcihad/pgxmigrate
```

CLI olarak kurmak icin:

```bash
go install github.com/mcihad/pgxmigrate/cmd/pgxmigrate@latest
```

Bu repoyu yayinlamadan once module adini kendi repo adresinle degistir:

```bash
go mod edit -module github.com/mcihad/pgxmigrate
go mod tidy
```

## Migration Dosyasi

Dosyalar varsayilan olarak `migrations/` dizininde `YYYYMMDDHHMMSS_migration_adi.sql` formatinda durur.

```sql
----------UP----------
CREATE TABLE users (
    id bigserial PRIMARY KEY,
    email text NOT NULL UNIQUE
);

----------DOWN----------
DROP TABLE users;
```

## CLI

CLI `.env` dosyasini otomatik okur. Baglanti icin once `--database-url`, sonra `DATABASE_URL`, sonra `DB_URL`, en son `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME/DB_SSLMODE` kullanilir.

```bash
pgxmigrate create create_users
pgxmigrate up
pgxmigrate up 1
pgxmigrate down
pgxmigrate down 2
pgxmigrate rollback
pgxmigrate redo
pgxmigrate reset
pgxmigrate to 20260603143000
pgxmigrate force 20260603143000
pgxmigrate status
pgxmigrate pending
pgxmigrate applied
pgxmigrate validate
pgxmigrate current
pgxmigrate delete 20260603143000
```

Ortak flag'ler:

```bash
pgxmigrate --dir database/migrations status
pgxmigrate --database-url "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" up
```

`up` argumansiz calisirsa tum bekleyen migrationlari uygular. `down` argumansiz calisirsa son migrationi geri alir. `rollback` son batch'i geri alir. `force` SQL calistirmadan migration tablosunu hedef versiyona ayarlar.

## Go Paketi Olarak Kullanim

Migration motoru public pakettedir ve pgxpool ister:

```go
package main

import (
    "context"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/mcihad/pgxmigrate/migrator"
)

func run(ctx context.Context, pool *pgxpool.Pool) error {
    m := migrator.New(pool, migrator.DefaultDirectory)

    if err := m.Ensure(ctx); err != nil {
        return err
    }

    _, err := m.Up(ctx, 0)
    return err
}
```

Kullanilabilir API:

```go
m.Create("create_users")
m.Delete(ctx, "20260603143000")
m.Up(ctx, 0)
m.Down(ctx, 1)
m.Rollback(ctx)
m.Redo(ctx)
m.Reset(ctx)
m.To(ctx, "20260603143000")
m.Force(ctx, "20260603143000")
m.Status(ctx)
m.Pending(ctx)
m.Applied(ctx)
m.Validate(ctx)
m.Current(ctx)
```

## Yayinlama

```bash
git init
git remote add origin git@github.com:cihad/pgxmigrate.git
git add .
git commit -m "Initial pgx migration package"
git push -u origin main
git tag v0.1.0
git push origin v0.1.0
```

Bir projede belirli surumu kullanmak icin:

```bash
go get github.com/mcihad/pgxmigrate@v0.1.0
```

Integration testleri calistirmak icin:

```bash
PGXMIGRATE_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test ./...
```
