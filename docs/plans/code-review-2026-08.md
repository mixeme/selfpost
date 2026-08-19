# Рецензирование кодовой базы SelfPost — август 2026

Дата: 2026-08-19.

---

## Общая оценка

SelfPost — зрелый, хорошо структурированный проект. Архитектура соответствует масштабу (self-hosted SMTP-реле с веб-панелью), код читабелен и поддерживаем, тестовое покрытие обширное (~60 тест-файлов), документация исключительно качественная, лицензионное соответствие AGPL-3.0 полное. Ниже — конкретные находки и план устранения.

---

## 1. Архитектура и структура проекта

**Оценка: отлично.** Стандартная Go-компоновка (`cmd/`, `internal/`, `test/e2e/`), чёткое разделение слоёв (store → service → handler → template). Два бинарника (`panel`, `selfpost-backup`), единый Docker-образ с supervisord. Всё соответствует масштабу проекта — нет ни over-engineering, ни архитектурных долгов.

---

## 2. Логические ошибки и баги

### 2.1 [Medium] SPF-checker не обрабатывает CIDR-нотацию
- **Файл:** `internal/dnscheck/spf.go`, `parseIPs()`
- **Суть:** `ip4:192.168.1.0/24` парсится как единичный IP `192.168.1.0`. Панель ложно предупреждает, что SPF не покрывает сервер.
- **Исправление:** использовать `net.ParseCIDR` + `IPNet.Contains()`.

### 2.2 [Low] Дублирующаяся проверка в user update (мёртвый код)
- **Файл:** `internal/web/handlers/handlers_users.go`, строки 225–239
- **Суть:** два последовательных блока проверяют одно условие (понижение последнего глобального админа). Второй блок недостижим.
- **Исправление:** оставить один блок с контекстным сообщением.

### 2.3 [Low] Вводящее в заблуждение flash-сообщение в настройках
- **Файл:** `internal/web/handlers/handlers_settings.go`, строки 73–94, 246–273
- **Суть:** при изменении username+email flash говорит «сессии сброшены», хотя сессии сбрасываются только при смене пароля. Мёртвые ветки `"username-email"`, `"password-email"` в `settingsFlash()`.
- **Исправление:** строить flash-сообщение из отдельных boolean-флагов.

### 2.4 [Low] Утечка резервации inflight при обрыве SMTP
- **Файл:** `internal/milter/inflight.go`
- **Суть:** если SMTP-соединение обрывается без `Abort()`, резервация живёт до TTL (60 сек). При высокой нагрузке возможен ложный отказ.
- **Исправление:** задокументировать компромисс в `docs/security.md`.

---

## 3. Отсутствующие CSS-определения на DMARC-страницах (баги GUI)

### 3.1 [Bug] 6 CSS-классов используются в шаблонах, но не определены в `panel.css`

| Класс | Шаблоны | Влияние |
|---|---|---|
| `.pair` | `dmarc.html`, `dmarc_domain.html`, `dmarc_report.html` | Контейнер-сетка не работает — карточки стакаются вертикально |
| `.attn` | `dmarc_report.html` (`.card.attn`) | Нет визуального выделения при fail > 0 |
| `.desk-only` | DMARC-страницы | Таблицы не скрываются на мобильных |
| `.phone-only` / `.phone-list` | `dmarc.html` | Мобильный список виден одновременно с таблицей на всех экранах |
| `.check-row` | `domain_detail.html` | Должно быть `.check` (определённый класс) |

**Исправление:** добавить отсутствующие правила в `panel.css`, включая media-запросы для `.desk-only`/`.phone-only`. Заменить `.pair` на `.split` или определить отдельно. Исправить `.check-row` → `.check`.

---

## 4. Безопасность

### 4.1 [Medium] Fail-open milter auth IP при ошибке БД
- **Файл:** `internal/milter/authip.go`
- **Суть:** при недоступности БД `authIPAllowed()` возвращает `true` — любой IP может отправлять.
- **Исправление:** оставить fail-open + добавить warning log.
- **Решение:** (b) fail-open с предупреждением.

### 4.2 [Medium] Нет ограничения stdin в DMARC-ingest
- **Файл:** `cmd/panel/dmarc_ingest.go`
- **Суть:** нет `io.LimitReader` — злонамеренное сообщение может исчерпать память.
- **Исправление:** `io.LimitReader(os.Stdin, 10<<20)`.

### 4.3 [Medium] Вытеснение bucket'ов в auth rate limiter
- **Файл:** `internal/web/auth/ratelimit.go`, `makeRoom()`
- **Суть:** при >10000 IP атакующий может вытеснить легитимные rate-limit записи, сбросив счётчики.
- **Исправление:** вытеснять bucket с ближайшим expiry, но заблокированные bucket'ы (failures >= max) не вытесняются до истечения TTL.

### 4.4 [Medium] Нет CSRF-токенов — только `Sec-Fetch-Site` / `Origin`
- **Файл:** `internal/web/security.go`
- **Суть:** защита зависит от браузера. В `docs/security.md` решение задокументировано с обоснованием, но defense-in-depth рекомендует токен.
- **Решение:** добавить per-session CSRF-токен.

### 4.5 [Low] CSP `unsafe-inline` для стилей
- **Файл:** `internal/web/security.go`
- **Исправление:** вынести inline-стили в stylesheet, удалить `'unsafe-inline'`.

### 4.6 [Low] `bcrypt.DefaultCost` (10) — рекомендуется 12+
- **Исправление:** определить `const bcryptCost = 12` и использовать везде.

### 4.7 [Low] Нет re-auth перед скачиванием backup
- **Файл:** `internal/web/handlers/handlers_backup.go`
- **Исправление:** запросить текущий пароль в форме backup.

### 4.8 [Very Low] Cookie без `__Host-` prefix
- **Файл:** `internal/web/auth/handlers.go`
- **Примечание:** в коде уже есть `Host-`-prefixed cookie при Secure, см. `auth_test.go`. Проверить, что покрытие полное.

### 4.9 [Low] zip-бомба в DMARC parse
- **Файл:** `internal/dmarc/parse.go`, `unzip()`
- **Исправление:** обернуть reader в `io.LimitReader`.

---

## 5. Дублирование кода

### 5.1 [Low] `writeFileAtomic` — две одинаковые реализации
- **Файлы:** `internal/domain/dkim.go`, `internal/postfix/write.go`
- **Исправление:** извлечь в `internal/atomicfile`.

### 5.2 [Low] `reloadViaSupervisor` — две реализации
- **Файлы:** `internal/postfix/postfix.go`, `internal/domain/opendkim.go`
- **Исправление:** извлечь в `internal/supervisor`.

### 5.3 [Low] `parseDomainRateLimitForm` / `parseAppRateLimitForm` — идентичная логика
- **Файл:** `internal/web/handlers/handlers_ratelimit.go`
- **Исправление:** объединить в `parseRateLimitForm`.

### 5.4 [Low] `applyDomainRateLimit` / `applyAppRateLimit`
- **Исправление:** параметризовать scope и saver.

### 5.5 [Very Low] `logf = log.Printf` — повторяется в 5+ пакетах
- **Исправление:** при переходе на `log/slog` (см. §7) проблема уйдёт.

### 5.6 [Very Low] `assertMapSafe` / `assertConfigSafe` — похожие проверки в нескольких пакетах
- **Исправление:** рассмотреть `internal/configsafe`.

---

## 6. Граничные случаи

### 6.1 [Low] `secretfile` принимает пустой пароль на уровне библиотеки
- **Исправление:** добавить `len(password) == 0` guard или задокументировать, что валидация на вызывающей стороне.

### 6.2 [Very Low] Fingerprint-коллизия при ротации лога
- **Файл:** `internal/logtail/offset.go`
- **Исправление:** включить inode в fingerprint.

### 6.3 [Low] Неограниченный вывод `postqueue -p`
- **Файл:** `internal/postfix/queue.go`
- **Исправление:** `io.LimitReader` или context timeout.

### 6.4 [Very Low] `parsePage` не ограничивает максимум
- **Файл:** `internal/web/handlers/handlers_monitor.go`
- **Исправление:** clamp к `lastPage`.

---

## 7. Рефакторинг и оптимизация

### 7.1 [Medium] Типизированные struct'ы для template data
- **Затронуто:** все `handlers_*.go`
- **Суть:** `map[string]any` с 40+ ключами (напр. `renderDomainDetail`) — хрупко, опечатки молчаливо дают zero-value.
- **Исправление:** определить struct'ы (`DomainDetailData`, `SettingsData` и т.д.).

### 7.2 [Medium] Структурированное логирование (`log/slog`)
- **Затронуто:** все пакеты
- **Суть:** нет уровней, структурированных полей, фильтрации.
- **Исправление:** инжектировать `*slog.Logger` через конструкторы.

### 7.3 [Low] Build tags для `/proc`-кода
- **Файл:** `internal/health/machine.go`
- **Исправление:** `//go:build linux` + stub для других платформ.

### 7.4 [Very Low] DNS resolver — 4 параллельных запроса вместо fallback
- **Файл:** `internal/dnscheck/resolver.go`
- **Исправление:** последовательный запрос с fallback, или задокументировать обоснование.

---

## 8. Документация

### 8.1 [Medium] CHANGELOG записывает удаление plan-файлов, которые ещё не удалены
- **Суть:** `[Unreleased]` Removed перечисляет 6 plan-файлов, но файлы ещё на диске.
- **Исправление:** проверить — файлы уже удалены; если да, пункт закрыт.

### 8.2 [Minor] `schema-migrations.md` — заголовок говорит v9, цепочка на v10
- **Исправление:** обновить заголовок и добавить индекс из миграции 0010.

### 8.3 [Low] ~40 ссылок на `spec N` в build/deploy-файлах
- **Суть:** CHANGELOG `[0.5.0]` говорит «код больше не ссылается на спецификацию», но shell-скрипты и Dockerfile не были очищены.
- **Исправление:** удалить ссылки на spec; где уместно — заменить на ссылку на актуальный документ.

### 8.4 [Low] `dmarc-ingest` symlink не документирован в `architecture.md`
- **Исправление:** добавить абзац в секцию Mail path.

### 8.5 [Low] Roadmap ссылается на plan-файлы, которые будут удалены
- **Исправление:** убрать ссылки на удалённые планы.

### 8.6 [Very Low] `dev/workflow.md` — устаревшая базовая версия
- **Исправление:** обновить «база 0.6.0» → актуальную.

---

## 9. Тесты

### 9.1 Общая оценка: сильное покрытие

Набор из ~60 тест-файлов покрывает: криптографию (`secretfile`), конкурентность (`milter` — 32 горутины), SQL edge cases (`store`), безопасность (CSRF-матрица, cookie-shadowing, injection-rejection, fail-open семантика), AGPL-compliance guards, e2e-сценарий. Тесты хорошо документированы комментариями.

### 9.2 Пакеты без тестов

| Пакет | Риск |
|---|---|
| `cmd/panel` (main.go, httpserver.go, journal.go, dmarc_ingest.go, ratelimit_recalc.go) | Low — startup-код, частично покрыт restore_test |
| `internal/dmarc` (service.go, addresses.go) | Medium — сервисный слой не протестирован |
| `internal/web/handlers` (handlers_apps.go, handlers_domains.go, handlers_users.go, handlers_dmarc.go, handlers_dmarc_reports.go, handlers_status.go, handlers_help.go) | Medium — нет прямых тестов (частично покрыты authz_test) |
| `internal/store` (store.go, sessions.go) | Low — sessions покрыты через auth_test |
| `internal/buildinfo` | Very Low — константы |

### 9.3 Слабые области тестирования
- `internal/postfix/dmarc_test.go` — только 2 теста, нет проверки injection
- `internal/web/validate/validate_email_test.go` — 4 кейса, тонкое покрытие
- `internal/store/stats_test.go` — нет edge case с нулевым трафиком

### 9.4 Отсутствуют тесты для
- Error page templates
- Corrupted backup archive handling
- Malformed XML в DMARC parse (oversized reports)
- Pagination edge cases в delivery log
- Concurrent store operations

---

## 10. GUI — дополнительные находки

### 10.1 [Low] Accessibility: help drawer
- Кнопка закрытия `<label class="help-close">` — нет `role="button"`.
- Scrim `<label class="help-scrim">` — нет `aria-label`.

### 10.2 [Low] Hardcoded URL guide
- `help.html` и `help_drawer.html` содержат захардкоженный GitHub URL вместо `{{.SourceURL}}`.
- **Решение:** формировать полный путь `{{.SourceURL}}/blob/main/docs/...`.

### 10.3 [Low] `.pair` vs `.split` — непоследовательность имён
- DMARC-шаблоны используют `class="pair"` для того же layout, что другие страницы называют `class="split"`.

### 10.4 [Very Low] Два `back_link` в `dmarc_domain.html`
- Необычный UX-паттерн (две ссылки «назад» подряд).

### 10.5 [Very Low] `initShowWhen` не вызывается после htmx swap
- Сейчас безопасно (формы не загружаются через htmx), но хрупко.

---

## 11. Соответствие лицензии AGPL-3.0

**Оценка: полное соответствие.** LICENSE, NOTICE, source offer в footer каждой страницы (включая login/setup), `/license` endpoint, OFL.txt для шрифтов, совместимость зависимостей проверена (BSD/0BSD/OFL). SPDX-заголовки per-file отсутствуют сознательно (задокументировано).

---

## План реализации

Приоритет: Critical → High → Medium → Low. Рекомендуемая модель для каждого пункта.

### Фаза 1 — Баги и безопасность (Sonnet)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 1 | Добавить отсутствующие CSS-классы для DMARC-страниц (§3) | Sonnet | 30 мин |
| 2 | Исправить SPF CIDR-парсинг (§2.1) | Sonnet | 30 мин |
| 3 | Добавить `io.LimitReader` в DMARC ingest и parse (§4.2, §4.9) | Sonnet | 15 мин |
| 4 | Исправить rate limiter eviction (§4.3) | Sonnet | 30 мин |
| 5 | Добавить logging в fail-open auth IP (§4.1) | Sonnet | 10 мин |
| 6 | Убрать мёртвый код в user demotion guard (§2.2) | Sonnet | 5 мин |
| 7 | Исправить flash-сообщения в настройках (§2.3) | Sonnet | 15 мин |
| 8 | `.check-row` → `.check` в domain_detail (§3.1) | Sonnet | 2 мин |

### Фаза 2 — Дублирование кода (Sonnet)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 9 | Извлечь `internal/atomicfile` (§5.1) | Sonnet | 20 мин |
| 10 | Извлечь `internal/supervisor` (§5.2) | Sonnet | 20 мин |
| 11 | Объединить rate-limit form parsing (§5.3–5.4) | Sonnet | 30 мин |

### Фаза 3 — Документация (Sonnet)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 12 | Удалить plan-файлы или исправить CHANGELOG (§8.1) | Sonnet | 5 мин |
| 13 | Обновить `schema-migrations.md` (§8.2) | Sonnet | 5 мин |
| 14 | Удалить ссылки на spec из build/deploy (§8.3) | Sonnet | 30 мин |
| 15 | Документировать dmarc-ingest symlink (§8.4) | Sonnet | 5 мин |
| 16 | Исправить ссылки в roadmap (§8.5) | Sonnet | 5 мин |

### Фаза 4 — Рефакторинг (Sonnet / Opus для `log/slog`)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 17 | Типизированные template data structs (§7.1) | Sonnet | 2–3 часа |
| 18 | Миграция на `log/slog` (§7.2) | Opus | 3–4 часа |
| 19 | Build tags для machine.go (§7.3) | Sonnet | 15 мин |
| 20 | Hardcoded guide URL → `{{.SourceURL}}` (§10.2) | Sonnet | 10 мин |

### Фаза 5 — Расширение тестов (Sonnet)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 21 | Тесты DMARC service layer | Sonnet | 1–2 часа |
| 22 | Тесты handler'ов (apps, domains, users, dmarc) | Sonnet | 3–4 часа |
| 23 | Дополнить postfix/dmarc_test injection cases | Sonnet | 30 мин |
| 24 | Дополнить validate_email_test | Sonnet | 20 мин |
| 25 | Тесты error pages и corrupted backup | Sonnet | 1 час |

### Фаза 6 — Опциональные улучшения (Low priority)

| # | Задача | Модель | Оценка |
|---|--------|--------|--------|
| 26 | Удалить `unsafe-inline` из CSP (§4.5) | Sonnet | 1 час |
| 27 | bcrypt cost 12 (§4.6) | Sonnet | 10 мин |
| 28 | Re-auth перед backup (§4.7) | Sonnet | 30 мин |
| 29 | Accessibility help drawer (§10.1) | Sonnet | 15 мин |
| 30 | `io.LimitReader` для postqueue (§6.3) | Sonnet | 10 мин |
| 31 | Extractить `internal/configsafe` (§5.6) | Sonnet | 30 мин |

---

## Статус реализации

Обновлено 2026-08-19 по факту проверки дерева (`main`).

| # | Задача | Статус |
|---|--------|--------|
| 1 | CSS для DMARC-страниц | сделано |
| 2 | SPF CIDR | не требовалось — `coversAny()` уже разбирает CIDR |
| 3 | `io.LimitReader` в DMARC ingest/parse | сделано |
| 4 | Вытеснение bucket'ов | сделано; плюс потолок роста карты заблокированных |
| 5 | Логирование fail-open auth IP | не требовалось — лог уже был |
| 6 | Мёртвый код в demotion guard | сделано |
| 7 | Flash-сообщения в настройках | сделано |
| 8 | `.check-row` | сделано иначе: класс определён в `panel.css` |
| 9 | `internal/atomicfile` | сделано |
| 10 | `internal/supervisor` | сделано |
| 11 | Объединение rate-limit парсеров | сделано; обёртки-заглушки удалены |
| 12 | Plan-файлы / CHANGELOG | сделано |
| 13 | `schema-migrations.md` | сделано: заголовок `v10`, битые ссылки убраны |
| 14 | Ссылки на spec в build/deploy | сделано: `build/`, `deploy/`, миграции, workflow |
| 15 | `dmarc-ingest` symlink | не требовалось — уже описан в `architecture.md` |
| 16 | Ссылки в roadmap | сделано |
| 17 | Типизированные template data structs | **не сделано** → `template-data-typing` |
| 18 | Миграция на `log/slog` | **не сделано** → `structured-logging` |
| 19 | Build tags для `machine.go` | отклонено, см. ниже |
| 20 | Guide URL → `{{.SourceURL}}` | сделано |
| 21 | Тесты DMARC service layer | **не сделано** → `review-2026-08-followups` |
| 22 | Тесты хендлеров | **не сделано** → `review-2026-08-followups` |
| 23 | postfix/dmarc injection cases | сделано |
| 24 | `validate_email_test` | сделано |
| 25 | Тесты error pages / битого бэкапа | **не сделано** → `review-2026-08-followups` |
| 26 | CSP `unsafe-inline` | не требовалось — директивы в CSP нет |
| 27 | bcrypt cost 12 | сделано, включая setup-аккаунт (`auth.BcryptCost`) |
| 28 | Re-auth перед бэкапом | **не сделано** → `review-2026-08-followups` |
| 29 | Accessibility help drawer | сделано |
| 30 | Ограничение `postqueue -p` | сделано: таймаут 5 с + лимит вывода 4 МиБ |
| 31 | `internal/configsafe` | сделано |

**Отклонено с обоснованием.** §7.3 (задача 19): чтение `/proc` параметризовано
корнем каталога и не содержит платформенного кода, его тесты разбирают
подставные деревья и проходят на любой ОС, а отсутствие `/proc` уже
обрабатывается штатно (`TestMachineSamplerWithoutProc`). `//go:build linux`
не даёт ни выигрыша при сборке, ни в рантайме, но убирает эти тесты на машине
разработчика.

**Открытый вопрос.** §4.4: решение «добавить per-session CSRF-токен» записано,
но задачи под него в таблицах фаз нет, и токенов в коде нет. Нужно либо
реализовать токены, либо вернуть §4.4 в статус осознанного компромисса и
оставить это решение в одном месте — в `security.md`.

**Вне фазовых таблиц** (в план не переносились и не делались): §5.5, §6.1,
§6.2, §6.4, §7.4, §8.6, §10.3, §10.4, §10.5, а также §9.3–9.4 сверх задач
23–25.

---

## Что перенесено в roadmap

Всё незакрытое заведено в [roadmap.md](../roadmap.md) — этот файл больше не
единственное место, где живут остатки:

| Пункт плана | Элемент roadmap |
|---|---|
| §4.4 CSRF-токен | [csrf-tokens](../roadmap.md#csrf-tokens) |
| Задача 17 (§7.1) | [template-data-typing](../roadmap.md#template-data-typing) |
| Задача 18 (§7.2), §5.5 | [structured-logging](../roadmap.md#structured-logging) |
| Задачи 21, 22, 25, 28 и §6.1, §6.2, §6.4, §7.4, §8.6, §9.3, §10.3–10.5 | [review-2026-08-followups](../roadmap.md#review-2026-08-followups) |
| Задача 19 (§7.3) | не переносится — отклонено, обоснование выше |

