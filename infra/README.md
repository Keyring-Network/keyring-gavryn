# Gavryn Infra

## Migrations
SQL migrations live in `infra/migrations/`.

Apply all migrations in order:

```sh
for migration in infra/migrations/*.sql; do psql "$POSTGRES_URL" -f "$migration"; done
```

Notes:
- `005_memory.sql` enables the `vector` extension for memory search.
