# SelfPost — разработка

**Что это.** Как собрать, проверить и выкатить изменения. Текущий спринт —
[progress.md](progress.md) (читать первым после `/clear`).

Границы продукта: [product.md](product.md). As-built устройство:
[architecture.md](architecture.md).

---

## Технологический стек и инструменты

| Компонент | Версия / заметки |
|---|---|
| **Go** | 1.26+ (`go.mod`); `CGO_ENABLED=0` — чистый Go, статическая линковка |
| **SQLite** | `modernc.org/sqlite` (pure Go, без cgo) |
| **Сборка** | [Makefile](../Makefile): `vet`, `test`, `build`, `e2e` |
| **Контейнер** | Docker + Compose v2 на хосте разработки и в CI |
| **Образ (build stage)** | `golang:1.26-bookworm` — [build/Dockerfile](../build/Dockerfile) |
| **Образ (runtime)** | `debian:bookworm-slim` + Postfix, OpenDKIM, supervisord, SASL, logrotate |
| **CI** | GitHub Actions — [.github/workflows/](../.github/workflows/) |
| **Реестр образов** | `ghcr.io/mixeme/selfpost` |

**Структура репозитория** (кратко; детали процессов — в [architecture.md](architecture.md)):

- `cmd/panel` — HTTP-панель + journal-milter + log-tailer
- `cmd/selfpost-backup` — CLI бэкапа (`docker exec … selfpost-backup`)
- `internal/` — доменная логика, store, web, health
- `build/` — Dockerfile, supervisord, Postfix/OpenDKIM, entrypoint
- `deploy/` — `docker-compose.yml`, примеры прокси, `.env.example`
- `test/e2e/` — **отдельный Go-модуль**; контейнерные интеграционные тесты

---

## Внешние библиотеки

Проект — **AGPL-3.0** ([LICENSE](../LICENSE)). Новые Go-зависимости — только
permissive или GPL-family (см. [.cursor/rules/agent-rules.mdc](../.cursor/rules/agent-rules.mdc)).

### Основной модуль (`go.mod`)

| Пакет | Версия | Репозиторий | Лицензия |
|---|---|---|---|
| `github.com/emersion/go-milter` | v0.4.1 | <https://github.com/emersion/go-milter> | BSD-2-Clause |
| `golang.org/x/crypto` | v0.54.0 | <https://github.com/golang/crypto> | BSD-3-Clause |
| `modernc.org/sqlite` | v1.53.0 | <https://gitlab.com/cznic/sqlite> (зеркало: <https://github.com/modernc-org/sqlite>) | BSD-3-Clause |

Транзитивные зависимости — `go mod graph` / `go.sum`; все indirect в дереве
совместимы с AGPL-3.0.

### Модуль e2e (`test/e2e/go.mod`)

| Пакет | Версия | Репозиторий | Лицензия |
|---|---|---|---|
| `github.com/emersion/go-msgauth` | v0.6.8 | <https://github.com/emersion/go-msgauth> | BSD-2-Clause |

Тестовый модуль не входит в граф основного `go build` и не попадает в образ.

### Пакеты Debian в runtime-образе

Postfix, OpenDKIM, `supervisord`, `sasl2-bin`, `logrotate` и др. —
из репозиториев Debian bookworm; лицензии — в `copyright` соответствующих
пакетов на <https://packages.debian.org/bookworm/>.

---

## Сборка исполняемого файла и образа

### Локальные бинарники

Требуется Go 1.26+ и `CGO_ENABLED=0`.

```sh
make build        # bin/panel, bin/selfpost-backup (VERSION=dev по умолчанию)
make build VERSION=1.0.0
```

Или напрямую:

```sh
go build -trimpath -ldflags "-X github.com/mixeme/selfpost/internal/buildinfo.Version=dev" -o bin/panel ./cmd/panel
```

Версия вшивается в оба бинарники через `-ldflags` и **должна совпадать с тегом
Docker-образа** — restore проверяет совместимость версий бэкапа.

### Docker-образ

Из корня репозитория:

```sh
docker build -f build/Dockerfile -t selfpost:dev --build-arg VERSION=dev .
```

В Dockerfile: build stage (`go vet`, `go build` с `VERSION`), runtime stage
(Debian + почтовый стек). См. [architecture.md](architecture.md) § Image and
processes.

---

## Сборка релиза

Релизный образ публикуется **только по тегу** `vX.Y.Z` (не на каждый push в
`main`). Тег — единственный источник версии: из него берутся тег образа и
`-ldflags` в бинарниках, чтобы они не расходились.

**Шаги (по явному запросу):**

1. Закрыть `[Unreleased]` в [CHANGELOG.md](../CHANGELOG.md).
2. Создать и запушить git-тег `vX.Y.Z`.
3. Workflow [release.yml](../.github/workflows/release.yml) собирает, гейтит
   e2e и публикует `ghcr.io/mixeme/selfpost:X.Y.Z`.
4. Обновить закреплённый тег в [deploy/docker-compose.yml](../deploy/docker-compose.yml)
   (см. [roadmap.md](roadmap.md) § «v1.x — хвост документации и деплоя»).

Обычные коммиты **не** публикуют образ.

---

## Тестирование

### Статический анализ и unit-тесты

Основной модуль (`go test ./...`); e2e — отдельный модуль, см. ниже.

```sh
make vet          # go vet ./...
make test         # go test ./...
```

Или напрямую:

```sh
gofmt -l .        # в CI — fail при расхождении
go vet ./...
go test ./...
```

### Регресс документации env

`go test ./cmd/panel -run TestLoadConfig` — каждый новый ключ `loadConfig`
должен появиться в списках env в [guide.md](guide.md)
([cmd/panel/envdoc_test.go](../cmd/panel/envdoc_test.go)).

### End-to-end (контейнерный suite)

Отдельный Go-модуль `test/e2e/`; **не** входит в `go test ./...` основного
модуля.

```sh
make e2e
# то же: cd test/e2e && go test -v -timeout 20m ./...
```

**Стек:** [deploy/docker-compose.yml](../deploy/docker-compose.yml) +
[test/e2e/compose.override.yml](../test/e2e/compose.override.yml) — те же
`cap_drop`/`cap_add`/`no-new-privileges`, что в production. Override: высокие
порты (`20465`/`20587`/`20080`), тестовый hostname, self-signed TLS,
`PANEL_COOKIE_SECURE=false`, изолированный compose-проект. Почта герметична:
CoreDNS (fake zone) + Postfix `smtp-sink` как sink-MX; DKIM TXT скрапится с
панели и публикуется в зону — тест проверяет записи, которые оператор реально
использует.

**Покрытие (сводка):** bootstrap → SMTP AUTH → доставка → DKIM verify →
send-log `queued → sent`; негативы (без AUTH, relay, sender/login mismatch,
L1/L2 лимиты, fail-open milter, неверный `SELFPOST_HOSTNAME`, сессия после
`docker restart`). Только polling с таймаутами — без фиксированных `sleep`.

Требует **Docker + Compose v2** на машине, где гоняется suite.

---

## CI

Workflows в [.github/workflows/](../.github/workflows/). Что именно гоняется —
в [§ Тестирование](#тестирование) выше.

### `test.yml` — каждый push и PR в `main`

`gofmt -l` → `go vet ./...` → `go test ./...` (основной модуль, без e2e).

### `release.yml` — push тега `vX.Y.Z` или `workflow_dispatch`

```
prepare (версия из тега)
  → build [matrix: ubuntu-latest / ubuntu-24.04-arm]
      → docker build --load (VERSION из тега)
      → e2e (test/e2e)
      → push ghcr.io/...:X.Y.Z-amd64 | X.Y.Z-arm64
  → merge
      → docker buildx imagetools create → единый манифест X.Y.Z
```

Нативная матрица per-arch (без QEMU): полный стек Postfix/OpenDKIM под
эмуляцией для e2e непрактичен. Сначала e2e, затем push — в registry попадают
байты, прошедшие гейт.

Провал e2e **блокирует** публикацию образа.
