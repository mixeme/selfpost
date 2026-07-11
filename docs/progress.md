# Прогресс реализации SelfPost

Живой трекер фаз. **Переживает `/clear`** — читается первым при возобновлении работы.
План фаз: [implementation-plan.md](implementation-plan.md). ТЗ: [specification.md](specification.md).

## Как возобновить после сброса контекста

1. Прочитать этот файл (текущая фаза, статус, что сделано, что дальше).
2. Прочитать соответствующую фазу в `implementation-plan.md`.
3. При необходимости — детали в `specification.md`.
4. Продолжить с пункта «Следующий шаг».

## Рекомендуемая модель по фазам

| Фаза | Модель | Почему |
|---|---|---|
| 0 — каркас + спайк milter | **Opus** | архитектура + главный технический риск (7.3) |
| 1 — Docker/supervisord/обёртка | **Opus** | тонкая логика холодного старта сокетов |
| 2 — SQLite/setup-link/auth | **Opus** | безопасность 7.6 (крипто-токен, сессии, bcrypt) |
| 3 — домены + OpenDKIM | **Opus** | генерация конфигов + exec-safety (7.6.3–4) |
| 4 — приложения + SASL + sender_login_maps | **Opus** | риск open relay / привязки отправителя |
| 5 — полный Postfix | **Opus** | самый чувствительный тракт доставки |
| 6 — journal-milter | **Opus** | наивысший риск (баг ломает релей) |
| 7 — UI мониторинга | **Sonnet** | шаблоны/CRUD, рутинно |
| 8 — rate limit L2 | **Opus** | логика лимитов в milter |
| 9 — бэкап/restore/экспорт | **Opus** | целостность данных, версионирование |
| 10 — деплой + docs | **Sonnet** | compose-файлы и документация |
| 11 — security-проход | **Opus** | аудит соответствия 7.6 |

Правило: безопасность / инфра / риск-критичное → **Opus**; UI / документация / бойлерплейт → **Sonnet**; тривиальная механика → **Haiku**.

## Коммиты

Коммит на **каждом осмысленном шаге** (не каждое сохранение файла, но и не только конец фазы): рабочий под-функционал, зелёная сборка, конец фазы. Минимум — один коммит на закрытую фазу + промежуточные на связные под-шаги. Ветка `main` (если пользователь не попросит отдельную). Push/PR — только по явной команде. Сообщение коммита завершается трейлером `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

## Протокол закрытия фазы

Перед `/clear` в конце каждой фазы Claude:
1. Обновляет этот файл: статус фазы → ✅, заполняет «Сделано» и «Следующий шаг».
2. Проверяет критерии «Готово, когда…» из плана.
3. Пишет одну строку в журнал ниже.
4. Делает финальный коммит фазы.
5. Явно говорит: «Фаза N закрыта — можно `/clear`, следующая фаза N+1 на модели X».

---

## Текущее состояние

- **Текущая фаза:** 2 ✅ закрыта → следующая **Фаза 3** (домены + OpenDKIM)
- **Модель для Фазы 3:** Opus (генерация конфигов + exec-safety 7.6.3–4, валидация имени домена 7.6.2)
- **Статус:** SQLite-персистентность, setup secret-link и вход админа реализованы и проверены на сервере (`go vet`/`build`/`test` зелёные, docker-образ собирается, контейнер поднимает три процесса и создаёт БД под `panel`)
- **Следующий шаг (Фаза 3):** добавить/список/удалить домен (при удалении — предупреждение о каскаде приложений, ТЗ 7.2.4); генерация DKIM-ключа + селектор per-domain (дефолт из `DKIM_SELECTOR_DEFAULT`), ключи в `/data/...` переживают рестарт (ТЗ 6, 9); `KeyTable`/`SigningTable` + reload OpenDKIM (сейчас `opendkim.conf` в Mode `v` без ключей — перевести в `s` + KeyTable); показ DKIM TXT-записи per-domain (ТЗ 7.2.10). **Безопасность:** строгая валидация имени домена (whitelist, 7.6.2), безопасная запись конфигов с экранированием (7.6.4), `os/exec` без shell и без интерполяции ввода (7.6.3). Таблицы `domains`/`applications`/`application_addresses` уже в схеме (миграция 0001).

### Сделано в Фазе 2
- **SQLite-персистентность** (`internal/store`): драйвер `modernc.org/sqlite` (чистый Go, без cgo — статик-бинарник сохранён; первые сторонние зависимости → появились `go.mod` require + `go.sum`), WAL + `foreign_keys(ON)` + `busy_timeout` через DSN `_pragma`, `MaxOpenConns(1)`. Встроенные (`embed`) нумерованные миграции с версионированием через `PRAGMA user_version`; миграция `0001_init.sql` заводит всю схему ТЗ 9: `admin` (одна строка, `CHECK id=1`), `settings`, `domains`, `applications`, `application_addresses`, `send_log` (+индексы), `rate_limits`. Запросы `AdminExists/CreateAdmin/GetAdmin`.
- **Setup secret-link** (`internal/web/setup.go`, ТЗ 7.6.1): токен 128 бит из `crypto/rand` (base64url), ссылка `https://<SELFPOST_HOSTNAME>/setup/<token>` печатается в лог **и** пишется в `/data/setup-token` (0600); TTL 10 мин с перегенерацией при истечении/рестарте (пока нет админа); сравнение `subtle.ConstantTimeCompare`; **неудачи НЕ инвалидируют токен**; «настройка завершена» = наличие строки `admin` (источник истины), поэтому после успеха токен сгорает навсегда и весь `/setup/*` → 404. Форма создания админа — одноразовая; гонка двух POST безопасна (`CHECK id=1` + проверка существования).
- **Пароль админа** — только `bcrypt` (`golang.org/x/crypto/bcrypt`, DefaultCost); серверная валидация (username whitelist 7.6.2, пароль ≥12).
- **Логин + сессии** (`internal/web/handlers_auth.go`, `session.go`): вход сверяет username + bcrypt (bcrypt считается всегда — timing-инвариантно), сессии в памяти (не в списке ТЗ 9 на персист — рестарт просто разлогинивает), крипто-токен 256 бит; cookie `HttpOnly`/`Secure`/`SameSite=Lax` (`Secure` по умолчанию, отключается `PANEL_COOKIE_SECURE=false` только для dev-HTTP); rate-limit логина (`ratelimit.go`, 10/15мин по IP) и отдельный на `/setup` (10/мин); auth-middleware защищает панель.
- **Front-end**: базовый layout `html/template` (автоэкранирование, 7.6.7) + страницы setup/login/dashboard (`embed`); вендоренный `htmx.min.js` 2.0.4 (`internal/web/static`, отдаётся с `/static/`).
- **Entrypoint для bind-mount** (`build/entrypoint.sh`): `/data` — host bind mount → приходит от root, а панель работает под непривилегированным `panel` (uid 999). Точка входа под root чинит владельца `/data` перед `exec supervisord` (иначе SQLite `unable to open database file`). Найдено и исправлено при контейнерной проверке.
- **Проверено на сервере** (selfpost.mixfed.ru): `go vet`/`go build`/`go test`/`gofmt -l` чисто; функциональный e2e (curl): setup-ссылка печатается+в файл, `GET /setup/<token>`→200, неверный/просроченный→404, `POST /setup` создаёт админа→303, повторный `/setup`→404, файл токена удалён; плохой логин→401, хороший→303 с cookie `HttpOnly; Secure; SameSite=Lax`, `/`→200, logout→303, после logout `/`→303; рестарт с существующим админом не печатает setup. Docker-образ собирается; контейнер с `-v ./data:/data`: три процесса, БД+токен создаются под `panel`, токен 0600.

### Сделано в Фазе 1
- **Образ** `build/Dockerfile` (bookworm-slim): многостадийная статическая сборка Go (`CGO_ENABLED=0`, `go vet` в сборке); рантайм — postfix, opendkim(+tools), cyrus-sasl (`sasl2-bin`, `libsasl2-modules`), supervisor, logrotate, ca-certificates; `maillog_file=/var/log/mail.log`; непривилегированный пользователь `panel` (ТЗ 7.6.8); `.dockerignore` (dev/ и docs/ не попадают в контекст).
- **supervisord** (`build/supervisord.conf`, `nodaemon`, PID 1): порядок `priority=` opendkim(100)→panel(200)→postfix(300); event-listener `crashexit.py` на `PROCESS_STATE_FATAL` шлёт SIGTERM супервизору → контейнер завершается при неперезапускаемом падении (ТЗ 4).
- **Обёртка Postfix** (`build/postfix-wrapper.sh`): опрос `test -S` обоих milter-сокетов (OpenDKIM `/run/opendkim/opendkim.sock` + journal `/run/selfpost/journal.sock`), таймаут 30с, при неготовности — выход ≠0 без запуска Postfix; иначе `exec postfix start-fg` (решает только холодный старт, ТЗ 4).
- **Панель** (`cmd/panel`, три роли под общим ctx + graceful shutdown по SIGTERM): HTTP `:8080` заглушка + `/healthz`; **stub journal-milter** — открывает unix-сокет, чтобы обёртка проходила (реальный milter — Фаза 6); **stub log-tailer** (реальный хвост — Фаза 6). `opendkim.conf` временно Mode `v` (без ключей; Mode `s`+KeyTable — Фаза 3).
- **Проверено на сервере** (selfpost.mixfed.ru): `docker build` ок; `docker run` → три живых процесса (opendkim, panel@non-root, postfix master); панель `/`→200 и `/healthz`→ok; обёртка дождалась сокетов; форсированный FATAL панели → crashexit → контейнер завершился чисто; SIGTERM → «panel stopped cleanly». Образ 334MB, тег `selfpost:dev` оставлен на сервере.

### Сделано в Фазе 0
- Go-модуль `codeberg.org/mix/selfpost`, каркас `cmd/panel` + `cmd/selfpost-backup`, общий `internal/buildinfo` (версия через ldflags).
- `Makefile` (статическая сборка `CGO_ENABLED=0`), `LICENSE` (полный AGPL-3.0), скелет `README.md`, `.gitattributes` (LF).
- Проверено на сервере: `go vet` чист, `make build` даёт статические бинарники, версия впечатывается.
- **Спайк go-milter** (де-риск ТЗ 7.3): v0.4.1 (BSD-2) ↔ Postfix 3.7.11 bookworm, протокол v6. Подтверждено чтение From/To(per-rcpt)/Subject/queue-id, client IP из Connect(), **fail-open** при падении milter'а. Детали: `dev/spike-milter-notes.md`.
- **Провижён dev-сервер:** Go 1.26.5, Docker 29.6.1, git, rsync, make. Host-postfix (спайковый) остановлен+отключён.

## Рабочая петля (dev loop) — ВАЖНО

Локально (Windows, `D:\Local\Git\selfpost`) **нет Go и Docker** — только редактирование и git. Вся сборка/тесты идут на dev-сервере `selfpost.mixfed.ru` (Debian 12 bookworm, тот же, что базовый образ; провижён под разработку). Цикл: править локально → `rsync` дерева на сервер → `go build`/`go vet`/`docker build`/тесты там. Источник истины и git-история — локальный репозиторий; сервер — только исполнитель сборки/тестов. Подключение: `ssh root@selfpost.mixfed.ru` (по ключу).

## Утверждённые решения

- Module path: `codeberg.org/mix/selfpost`
- SQLite: `modernc.org/sqlite` (чистый Go, без cgo)
- milter: `github.com/emersion/go-milter` (проверить совместимость в Фазе 0)
- Структура: стандартная Go-раскладка (`cmd/`, `internal/`)
- Front-end: `html/template` + вендоренный HTMX
- Коммиты — только по явной команде пользователя (ТЗ 12.1)

## Журнал фаз

- **Фаза 0** (2026-07-11, Opus) — каркас проекта + build-пайплайн + спайк go-milter (риск ТЗ 7.3 снят). Коммиты `4e589e1` (каркас), `87388b4` (план).
- **Фаза 1** (2026-07-11, Opus) — Docker-образ + supervisord + три процесса, холодный старт (обёртка ждёт milter-сокеты) и crashexit проверены на сервере. Коммит `ed9e942`.
- **Фаза 2** (2026-07-11, Opus) — SQLite (`modernc.org/sqlite`, миграции, схема ТЗ 9), setup secret-link (128-бит токен, TTL 10м, const-time, одноразово), bcrypt-админ, логин/сессии/cookie-флаги, rate-limit setup+логина, html/template + вендоренный HTMX, auth-middleware; entrypoint чинит владельца bind-mount `/data`. Проверено на сервере (e2e curl + docker run).
