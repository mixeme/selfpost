# План документации SelfPost

**Статус: закрыт (D1–D9, август 2026).** Проход выполнен; история задач и
находок — в [CHANGELOG.md](../CHANGELOG.md) и `git log`. Этот файл дальше
держит **состав пакета**, **метод сверки с кодом** и **правила**, чтобы
документация не разошлась снова.

**Живые документы (вместо архивного ТЗ):**

| Дом | Файл |
|---|---|
| Пользовательская поставка | [README.md](../README.md) |
| Границы продукта | [product.md](product.md) |
| As-built устройство | [architecture.md](architecture.md) |
| Процесс разработки | [development.md](development.md) |
| Безопасность | [security.md](security.md) |
| Исторический снимок v1.0 | [archive/specification-v1.0.md](archive/specification-v1.0.md) |

Отложенная полировка v1.x (Quick start на Codeberg, тег образа в compose,
`docs/logo`) — [roadmap.md](roadmap.md) § «v1.x — хвост документации и деплоя».

---

## 1. Состав пакета

### Поставляемое пользователю

| Артефакт | Состояние |
|---|---|
| [README.md](../README.md) | Установка, площадка, DNS, прогрев IP, эксплуатация, rate limiting, env, бэкап, репозиторий, образ, лицензия |
| [LICENSE](../LICENSE) | AGPL-3.0, полный текст |
| [deploy/docker-compose.yml](../deploy/docker-compose.yml) + прокси | Apache + nginx/Caddy/Traefik в [deploy/](../deploy/) |
| [deploy/.env.example](../deploy/.env.example) | Публичные переменные; полный справочник в README |
| [CHANGELOG.md](../CHANGELOG.md) | Keep a Changelog |

### Рабочие документы

[progress.md](progress.md), [implementation-plan.md](implementation-plan.md),
[roadmap.md](roadmap.md), этот файл.

**Вне объёма v1.x:** `CONTRIBUTING.md`, man-страницы, отдельный сайт документации.

---

## 2. Метод сверки с кодом

Правило: у каждого утверждения в документации есть **источник истины в дереве**;
сверка идёт от кода к тексту.

| Класс утверждений | Источник истины |
|---|---|
| Env-переменные, дефолты | `loadConfig` — [cmd/panel/main.go](../cmd/panel/main.go); `${VAR:-…}` в [build/](../build/) |
| Почтовый тракт | [build/postfix-config.sh](../build/postfix-config.sh) |
| Маршруты панели | [internal/web/web.go](../internal/web/web.go) |
| Бэкап/restore, экспорт домена | [internal/backup/](../internal/backup/), [cmd/selfpost-backup/](../cmd/selfpost-backup/) |
| Сессии | [internal/store/sessions.go](../internal/store/sessions.go), [internal/web/session.go](../internal/web/session.go) |
| Ротация лога, reload | [build/logrotate-mail.conf](../build/logrotate-mail.conf), [build/logrotate-loop.sh](../build/logrotate-loop.sh), [build/postfix-cert-reload.sh](../build/postfix-cert-reload.sh) |
| Деплой | [deploy/docker-compose.yml](../deploy/docker-compose.yml), [build/Dockerfile](../build/Dockerfile) |
| Чеклист README | таблица «Поставляемое пользователю» выше |
| Продукт, out of scope | [product.md](product.md) |
| As-built | [architecture.md](architecture.md) |
| Обязательная безопасность | [security.md](security.md) |

Порядок: перечислить фактическое в коде → найти в README / `architecture.md`.
Перед каждым тегом — короткий проход по этой таблице, не полная ревизия текста.

---

## 3. Правила поддержки

1. **Правило шага:** новая/переименованная env-переменная, маршрут панели или
   наблюдаемое поведение почтового тракта закрываются вместе с README /
   `.env.example` и записью в CHANGELOG (протокол — [progress.md](progress.md)).
2. **Регресс env (D7):** [cmd/panel/envdoc_test.go](../cmd/panel/envdoc_test.go) —
   падает на недокументированном ключе `loadConfig` или build-скриптов.
3. **Новые расхождения** дописываются в [roadmap.md](roadmap.md) или
   [implementation-plan.md](implementation-plan.md), а не исправляются молча.

---

## 4. Гейт релиза (документация)

Документационный проход **D1–D9 закрыт.** До тега релиза остаются другие пункты
общего гейта: e2e (C.4), ревизия безопасности (D.5 в
[implementation-plan.md](implementation-plan.md)) — см. [progress.md](progress.md).
