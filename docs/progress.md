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

- **Текущая фаза:** 4 ✅ закрыта → следующая **Фаза 5** (полная конфигурация Postfix — исходящий релей)
- **Модель для Фазы 5:** Opus (самый чувствительный тракт доставки: SASL/TLS на 465, `reject_sender_login_mismatch`, отсутствие open relay)
- **Статус:** приложения + SASL + привязка к домену реализованы и проверены на сервере (`gofmt`/`go vet`/`go test` зелёные, docker-образ собирается; контейнерный e2e: создание wildcard/list-приложений с показом пароля один раз, валидация (чужой домен → 400, дубль логина → 409, `@` в логине → 400), учётки в `sasldb2` под realm, генерация `smtpd_sender_login_maps` с many-to-one слиянием логинов, перевыпуск пароля, смена режима, удаление приложения, каскадное удаление домена с очисткой `sasldb2` и пересборкой карты, персистентность `sasldb2`+карты через рестарт с самолечением прав, **реальный `postfix reload`** через одноразовую supervisord-программу)
- **Следующий шаг (Фаза 5):** `master.cf` — `smtps` 465 (wrapper TLS) как основной, опционально `submission` 587 (STARTTLS, `smtpd_tls_auth_only=yes`); `main.cf` — `smtpd_sasl_auth_enable`, `smtpd_sasl_type=cyrus` + `smtpd/sasl` conf, указывающий на `/data/sasl/sasldb2`; **`smtpd_sender_login_maps = texthash:/data/postfix/sender_login_maps`** (файл уже генерируется панелью в Фазе 4) + `reject_sender_login_mismatch` в `smtpd_sender_restrictions`; `smtpd_recipient_restrictions` (`permit_sasl_authenticated`, `reject_unauth_destination`), никакого open relay; TLS cert/key из `TLS_CERT_FILE`/`TLS_KEY_FILE` (read-only mount) + периодический reload; исходящая доставка (MX-lookup, `smtp_tls_security_level=may`); rate-limit уровня 1 (`anvil`); milter-цепочка OpenDKIM + journal (fail-open). **Критично для Фазы 5:** определиться с реалмом SASL — панель создаёт учётки под realm `SELFPOST_HOSTNAME` (см. `SASL_REALM`); значение в карте — «голый» логин. Настроить Postfix так, чтобы `sasl_username` совпадал со значением карты (`smtpd_sasl_local_domain` пустой ИЛИ подгонка realm), и проверить реальной отправкой. Также решить chroot для `smtpd`: `/data/sasl/sasldb2` вне `/var/spool/postfix` — либо отключить chroot для сервиса приёма, либо иначе обеспечить доступ.

### Сделано в Фазе 4
- **Модель приложения** (`internal/store/applications.go`): CRUD над `applications`/`application_addresses` в транзакциях; `login` глобально уникален (арбитр — UNIQUE, `ErrLoginExists`); `ListBindings` (SQL `UNION ALL`) отдаёт пары «адрес→логин» (wildcard → `@domain`, list → каждый адрес) детерминированно отсортированными — сырьё для карты; `ListLoginsByDomain` для очистки `sasldb2` перед каскадом; `DeleteApplication` возвращает удалённую строку (нужен логин для `sasldb2`).
- **SASL `sasldb2`** (`internal/app/sasl.go`): обёртка над `saslpasswd2` (эквивалент по ТЗ 5.1). `Set` = `-p -c -f <db> -u <realm> <login>`, **пароль только через stdin** (не в argv → не течёт в `ps`/логи), логин — отдельный argv-элемент после строгого whitelist (`validateLogin`, без `@` — это разделитель realm в `sasldb2`), без shell (ТЗ 7.6.3). `Delete` = `-d ...`, идемпотентно. `run` — инъектируемое поле для тестов.
- **Пароль приложения** (`internal/app/password.go`): 24 байта `crypto/rand` → base64url (192 бита), генерируется панелью, показывается **один раз**, plaintext не хранится (ТЗ 7.6.1).
- **Валидация** (`internal/app/validate.go`): `validateLogin` (3–64, `[A-Za-z0-9._-]`, без `@`); `validateSenderAddress` — **критичная проверка ТЗ 7.6.2**: часть после `@` обязана строго равняться домену приложения (проверка ДО записи в конфиг, а не через `sender_login_maps` при доставке); строгий whitelist localpart; `parseAddresses` нормализует/дедуплицирует/требует ≥1 адрес.
- **`smtpd_sender_login_maps`** (`internal/postfix/postfix.go`): полная регенерация файла из всех привязок (чистая функция реестра, идемпотентно, как OpenDKIM-таблицы), атомарная запись (`write.go`, temp+rename). **Many-to-one**: несколько логинов на один адрес сливаются в одну строку `адрес log1,log2` (штатный случай ТЗ 5.1 §4). `assertMapSafe` — backstop против пробелов/переводов строк/запятых/`@` в логине (ТЗ 7.6.4). Тип карты для Фазы 5 — `texthash:` (без `postmap`).
- **Reload Postfix — исправленный механизм** (ключевое инфра-решение): изначальный план «`supervisorctl signal HUP postfix`» **не работает** — `postfix start-fg` форкает отдельный master, и сигнал супервизируемому foreground-процессу до master не доходит (в отличие от OpenDKIM, который сам и есть foreground-процесс). Решение: одноразовая supervisord-программа `[program:postfix-reload]` (`command=/usr/sbin/postfix reload`, `autostart=false`, `startsecs=0`, `exitcodes=0`); панель дёргает её `supervisorctl start postfix-reload` через тот же групповой контрол-сокет. Даёт настоящий `postfix reload` от root без привилегий у панели; postfix остаётся RUNNING, программа уходит в EXITED(0), crashexit не срабатывает. Проверено по `mail.log` (`reload -- version`).
- **Сервис приложений** (`internal/app/service.go`): координация store+`sasldb2`+карты. `Create` — сначала строка реестра (арбитр дубля, не даёт затереть чужой пароль в `sasldb2`), затем `sasldb2`, затем rebuild карты + reload; полный откат при сбое любого шага. `UpdateMode`/`Delete`/`RegeneratePassword`/`PurgeDomainSASL`/`Resync`. `SenderMaps` — интерфейс над Postfix для тестируемости.
- **Интеграция с доменами** (`internal/domain/service.go`): `Delete` теперь через интерфейс `Applications` — сначала `PurgeDomainSASL` (пока логины в реестре), затем каскад БД, затем rebuild карты + reload Postfix, затем удаление DKIM-ключа. Ручной reload (7.2.12) теперь перегружает и OpenDKIM, и Postfix.
- **Web** (`internal/web/handlers_apps.go`, шаблон `domain_detail.html`): создание/редактирование режима/перевыпуск/удаление приложения в карточке домена; одноразовый показ пароля рендерится **инлайн** (не через redirect — иначе пароль потерян); серверные ошибки валидации показываются на форме; флаги подтверждения на удаление/перевыпуск.
- **Инфра**: `postfix` добавлен в группу `selfpost` (Dockerfile) для чтения `sasldb2`/карты; `entrypoint.sh` нормализует `/data/sasl` (setgid 2750, `sasldb2` 0640) и `/data/postfix` (файл карты создаётся пустым до старта, 0640) — самолечение прав после restore.
- **Проверено на сервере** (selfpost.mixfed.ru): `gofmt`/`go vet`/`go test` зелёные (юниты: store CRUD/bindings/cascade, рендер карты+инъекции, валидация логина/адреса/принадлежности домену, argv/stdin `saslpasswd2`, сервис create/rollback/delete/mode/regen/purge с фейками); docker build ок; контейнерный e2e — весь жизненный цикл приложения + каскад домена + персистентность через рестарт + **реальный `postfix reload`** из пути создания и кнопки reload.

### Сделано в Фазе 3
- **Per-domain DKIM в чистом Go** (`internal/domain/dkim.go`): RSA-2048 через `crypto/rsa`, приватный ключ PKCS#1 PEM пишется атомарно (temp+rename, `writeFileAtomic`) с mode 0640; DNS TXT-запись (`v=DKIM1; h=sha256; k=rsa; p=<base64 PKIX DER>`) вычисляется из ключа на лету — ключ на диске = единственный источник истины. `opendkim-genkey` **не** используется (никакого exec для keygen).
- **OpenDKIM KeyTable/SigningTable** (`internal/domain/opendkim.go`): полная регенерация обеих таблиц из реестра при каждом add/delete (идемпотентно), атомарная запись; `KeyTable` — абсолютный путь к ключу (OpenDKIM резолвит относительные от CWD — проверено), `SigningTable` через `refile:` с шаблоном `*@domain`. `assertConfigSafe` (backstop 7.6.4) отклоняет пробелы/переводы строк/`:`/`/` в имени/селекторе перед записью.
- **Reload OpenDKIM без root** (ключевое инфра-решение): панель (uid 999, `panel`) **не может** сигналить процесс opendkim (uid 100) напрямую. Reload = `supervisorctl signal USR1 opendkim` (exec, фикс-аргументы, без shell/ввода — 7.6.3); контрол-сокет supervisord открыт группе (`chown=root:selfpost chmod=0770`), `panel` в группе `selfpost`. `ExecReload` opendkim = `kill -USR1` (SIGUSR1 перечитывает таблицы) — подтверждено.
- **Межпользовательский доступ к ключам:** общая группа `selfpost` (в неё добавлены `panel` и `opendkim`); дерево `/data/opendkim` с setgid (mode 2750) → файлы, созданные панелью, наследуют группу `selfpost`; ключи 0640 → opendkim читает по группе. `RequireSafeKeys no` (ключи group-readable — безопасно на приватном single-tenant bind-mount). `entrypoint.sh` нормализует дерево на каждом старте (group/​setgid/​пермишены + пустые KeyTable/SigningTable до старта opendkim), самолечение после restore. `opendkim.conf` переведён Mode `v`→`s`.
- **Сервис/веб** (`internal/domain/service.go`, `internal/store/domains.go`, `internal/web/handlers_domains.go`): Add (реестр→ключ→rebuild→reload, откат строки при сбое; существующий ключ переиспользуется, не перегенерируется — иначе сломается опубликованный DNS), Delete (каскад приложений через FK `ON DELETE CASCADE` + удаление ключа + rebuild+reload), список на дашборде со счётчиком приложений, карточка домена с TXT-записью (7.2.10), отдельная страница подтверждения удаления с предупреждением о каскаде (7.2.4), ручной Reload (7.2.12, пока только OpenDKIM — Postfix в Фазе 5). Валидация имени домена — строгий whitelist `[a-z0-9.-]`, DNS-форма, ≥2 меток (7.6.2), нормализация в lower-case. Роутинг переведён на authenticated под-mux с method+wildcard-паттернами Go 1.22.
- **Проверено на сервере** (selfpost.mixfed.ru): `gofmt`/`go vet`/`go test` (юниты: validateDomain, DKIM keygen/record roundtrip, renderTables + injection-safety, EnsureKey reuse, store cascade) зелёные; `docker build` ок; контейнерный e2e (curl+сессия): три процесса живы, add `Example.COM`→нормализация→ключ+таблицы+TXT, opendkim читает ключ панели и остаётся RUNNING после reload, **рестарт → ключ и таблицы персистентны (хэш совпал)**, delete → каскад, удаление ключа, пустые таблицы, opendkim reload; лог панели без ошибок.

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
- **Фаза 3** (2026-07-11, Opus) — домены + per-domain DKIM: keygen в чистом Go (RSA-2048, PKCS#1 PEM, TXT из ключа), OpenDKIM KeyTable/SigningTable (Mode `s`, `refile:`) с полной регенерацией, reload через `supervisorctl signal USR1 opendkim` (сокет открыт группе `selfpost`, панель без root — 7.6.3/7.6.8), межпользовательский доступ к ключам через общую группу `selfpost`+setgid+`RequireSafeKeys no`, add/delete/список/TXT/подтверждение каскада/ручной reload, строгая валидация имени домена (7.6.2), injection-safe запись таблиц (7.6.4). Юнит-тесты + контейнерный e2e (add/delete, персистентность ключей через рестарт, reload) зелёные. Проверено на сервере.
- **Фаза 4** (2026-07-12, Opus) — приложения + SASL + привязка к домену: учётки в `sasldb2` через `saslpasswd2` (пароль по stdin, логин whitelisted argv, без shell — 7.6.3), генерируемый пароль показывается один раз (7.6.1), режим адресов wildcard/list с серверной проверкой принадлежности адреса домену (7.6.2), генерация `smtpd_sender_login_maps` (many-to-one слияние, injection-safe — 7.6.4), CRUD приложений + перевыпуск пароля + каскад при удалении домена (очистка `sasldb2` + пересборка карты). **Исправлен reload Postfix:** `signal HUP` не доходит до форкнутого master → одноразовая supervisord-программа `postfix-reload` (настоящий `postfix reload` от root без привилегий панели). `postfix` в группе `selfpost`, `/data/sasl`+`/data/postfix` под setgid. Новые пакеты `internal/app`, `internal/postfix`. Юнит-тесты + контейнерный e2e (весь жизненный цикл, каскад, персистентность, реальный reload по `mail.log`) зелёные.
