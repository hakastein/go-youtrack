# Изменения

## v0.2.0

Ломающие:

- `Client.Fields` заменён на `Client.Metadata` и `Client.ReadMetadata`: ответ `*Metadata` с `Fields`, `FromCache`
  и `Request`; `Metadata` отвечает из кэша, `ReadMetadata` читает сервер и кладёт в кэш.
- `Issue` несёт `Description` и `Links`, `Value` — `LocalizedName`; `fields=` чтения задачи и ответа на запись
  расширены: `description`, `links(direction,linkType(name,sourceToTarget,targetToSource),issues(id,idReadable))`,
  `value(…,localizedName)`.
- Кэш метаданных хранит `canBeEmpty`; файлы кэша `v0.1.0` читаются без него.

Новое:

- `FieldType.Encode`, `FieldType.ReadValue`, `FieldType.Named`, `FieldType.Same`, `FieldType.BundleFields` и
  `ValueKeys`: знание о полях для потребителя, который собирает запросы сам.
- `Link`, `LinkType`, `Direction`, `IssueRef`, `Encoded`, `Metadata`.

## v0.1.0

Транспорт, сгенерированный клиент `ytapi`, таблица типов кастом-полей, `Issue`, `WriteFields`, `Bundle`,
`Fields`, `Users`, кэш метаданных и фейковый сервер `fake`.
