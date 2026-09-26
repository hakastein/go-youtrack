---
status: accepted, implemented
---

# Генератор владеет поверхностью операций, тела пишет модуль

Сгенерированная структура не отправит `null` и не отличит «не запрошено» от «пусто».

- `oapi-codegen` с `models: true`, `client: true`; `client-with-responses.tmpl` — заглушка. Тела — через `<Op>WithBody`.
- `ytapi` публичный (`Client.API()`); в модуле его зовёт только `ytapi.go`.
- `api/openapi.json` снимает `make openapi` с YouTrack 2026.1.13757 (`YOUTRACK_URL`). Руками не правится.
- Правки спецификации — только `api/overlay.yaml` (`strict: true`, без новых схем): `x-go-name`, `operationId`,
  исправленный параметр и измеренные необъявленные: `customFields=` у `GET /issues/{id}`, `query=` у `GET /users`
  (начало логина и полного имени) и у `GET /articles`.
- `make ytapi`: пакет собирается, выход и `catalogue.gen.go` воспроизводятся байт в байт, `ClientWithResponses` нет,
  в `ClientInterface` ≥ 335 методов, `ytapi` импортирует только `ytapi.go`.

## Отвергнуто

- `ClientWithResponses` и типизированные тела.
