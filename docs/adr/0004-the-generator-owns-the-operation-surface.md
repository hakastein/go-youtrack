---
status: accepted, implemented
---

# Генератор владеет поверхностью операций, тела пишет модуль

Сгенерированная структура не отправит `null` и не отличит «не запрошено» от «пусто».

- `oapi-codegen` запускается с `models: true` и `client: true`, шаблон `client-with-responses.tmpl` заменён заглушкой.
  Тела запросов модуль собирает сам и шлёт через `<Op>WithBody`.
- `ytapi` публичный, его отдаёт `Client.API()`; внутри модуля его зовёт только `ytapi.go`.
- `api/openapi.json` снимает `make openapi` с YouTrack 2026.1.13757 (`YOUTRACK_URL`); руками он не правится.
- Спецификацию правит только `api/overlay.yaml` (`strict: true`, новых схем не добавляет): `x-go-name`, `operationId`,
  исправление объявленного параметра и измеренные параметры, которых спецификация не объявляет: `customFields=` у
  `GET /issues/{id}`, `query=` у `GET /users` (ищет по началу логина и полного имени) и у `GET /articles`.
- `make ytapi` проверяет: пакет собирается, `ytapi/` и `catalogue.gen.go` воспроизводятся байт в байт,
  `ClientWithResponses` нет, в `ClientInterface` не меньше 335 методов, `ytapi` импортирует только `ytapi.go`.

## Отвергнуто

- `ClientWithResponses` и сгенерированные типы тел.
