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

- **Текущая фаза:** 1 ✅ закрыта → следующая **Фаза 2** (SQLite + setup-link + вход админа)
- **Модель для Фазы 2:** Opus (безопасность 7.6: крипто-токен, сессии, bcrypt)
- **Статус:** образ собирается и проверен на сервере; три процесса живы, холодный старт и crashexit подтверждены
- **Следующий шаг (Фаза 2):** SQLite-схема + миграции (домены, приложения, админ, `send_log`, лимиты, настройки, флаг «настройка завершена»/setup-токен), единый корень `/data`; **setup secret-link** (токен ≥128 бит `crypto/rand`, TTL 10 мин с перегенерацией, rate-limit маршрута, `subtle.ConstantTimeCompare`, неудачи НЕ инвалидируют токен, одноразовая форма создания админа, инвалидация навсегда после успеха → `/setup/*` 404); bcrypt-хэш пароля админа; логин + сессии (крипто-токен, cookie `HttpOnly`/`Secure`/`SameSite`), rate-limit логина; базовый layout `html/template` + вендоренный HTMX + auth-middleware. Выбрать драйвер `modernc.org/sqlite` (первая сторонняя зависимость — появится `go.sum`). Проверка: первый запуск печатает setup-ссылку, админ создаётся один раз, повторный `/setup` → 404, вход/выход работают.

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
