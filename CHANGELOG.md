# Изменения

## v0.5.0

Своих полей по умолчанию у операций с `Fields` нет: выражение полей всегда пишет вызывающий.

Ломающие:

- Выражение полей документной операции обязательно: пустое, в том числе при `nil` вместо опций, и начатое с `+` —
  `bad_usage` до запроса.
- Удалены наборы полей по умолчанию: `IssueShowFields`, `IssueListFields`, `ArticleShowFields`, `ArticleListFields`,
  `CommentFields`, `CommentListFields`, `AttachmentListFields`, `LinkListFields`, `TagListFields`, `TagCreateFields`,
  `WorkItemListFields`, `WorkItemWriteFields`, `ActivityListFields`, `ProjectShowFields`, `ProjectListFields`,
  `FieldListFields`, `UserShowFields`, `UserListFields`.
- `Fields.Show` не подбирает поля по типу поля и читает поле типа, которого модуль не знает; `FieldType.BundleFields`
  удалён. Выражение с `bundle(values(name,archived),aggregatedUsers(login))` читает бандл любого типа: имя, которого
  у типа поля нет, опускается, а у поля пользователей под `values` приходят люди и группы бандла.
- Каждое имя под `customFields` сверяется с каталогом кастом-полей инстанса: имя, которого у инстанса нет, —
  `unknown_name`.

Новое:

- `ytapi.UserBundle.Values`: сервер присылает у бандла пользователей его людей и группы, а спецификация этого не знала.
- `WithMetadataCache` кэширует и каталог кастом-полей инстанса: имя, которого в кэше нет, перечитывает каталог, а
  обычный вызов обходится без его запроса.

## v0.4.0

Ломающие:

- Модуль переименован: путь импорта — `github.com/hakastein/go-youtrack`, пакет по-прежнему `youtrack`. API тот же,
  что у `v0.3.0` на `github.com/hakastein/youtrack`.

## v0.3.0

Операции над сущностями — методы сервисов на клиенте.

Ломающие:

- `New` заменён на `NewClient`; операции — методы сервисов клиента:
  - `Client.Issue(ctx, id, fields)` → `Client.Issues.Get(ctx, id)`: выражения полей у чтения больше нет, `Issue.Tree`
    удалён — ответ под выражением вызывающего отдаёт `Issues.Show` документом;
  - `Client.WriteFields` → `Client.Issues.WriteFields`;
  - `Client.Bundle`, `Client.Metadata`, `Client.ReadMetadata` → `Client.Fields.Bundle`, `Metadata`, `ReadMetadata`;
  - `Client.Users(ctx, query, limit)` → `Client.Users.Find(ctx, query, limit)`; `limit` 0 — `DefaultLimit`.
- Десять типов ошибок (`ArgumentError`, `TransportError`, `StatusError`, `ResponseError`, `MismatchError`,
  `FieldNameError`, `ValueError`, `RequiredFieldError`, `PermissionError`, `ChangedFieldError` и их `Mismatch`,
  `AmbiguousName`, `InvalidValue`) заменены одной `*Error` с `Code`, `Message`, `Details`, `AfterWrite` и `Err`.
  Код — из закрытого словаря (`bad_usage`, `unknown_name`, `missing_required`, `not_found`, `denied`, `rejected`,
  `upstream_failed`, `upstream_invalid`, `write_uncertain`), сентинелы `Err…` сравниваются `errors.Is` по коду,
  `MayHaveWritten` заменяет `TransportError.Written`, `StatusError.Uncertain` и `ResponseError.Write`, `AfterWrite` —
  `StatusError.Accepted`.
- `Send`, `FieldType.Encode` и `FieldType.ReadValue` отвечают `*Error`: тип вне таблицы у обоих —
  `upstream_invalid`, и `ReadValue` отвергает `id` или `localizedName` значения, которые не строка и не `null`.
- Тип `Request` и `Metadata.Request` удалены: запрос стоит в `Details` ошибки под `request`.
- `fake.Server.Env()` удалён.
- Токен больше не попадает в текст ошибки `NewClient`.

Новое:

- Документ `Node` с видами `NullNode`, `StringNode`, `NumberNode`, `BoolNode`, `TextNode`, `ListNode`, `MapNode`,
  парами `Pair` и `DataPair`, аксессорами `Kind`, `Value`, `Items`, `Pairs`, `Lookup` и `MarshalJSON`; `CheckKey`.
- Сервисы `Issues`, `Articles`, `Comments`, `Attachments`, `Links`, `Tags`, `WorkItems`, `Activities`, `Projects`,
  `Fields`, `Users` с документными операциями `Show`, `List`, `Children`, `Create`, `Update`, `Delete`, `Add`, `Remove`
  и типизированными `Issues.Get`, `Issues.WriteFields`, `Fields.Metadata`, `Fields.ReadMetadata`, `Fields.Bundle`,
  `Users.Me`, `Users.Find`.
- Выражение полей вызывающего с кастом-полями в кавычках и `+x` к набору по умолчанию, проверка имён по `$type`
  каталогом схем из спецификации (`catalogue.gen.go`, генератор `scripts/catalogue.go`; `make ytapi` сверяет и его),
  страница списка `Page` с `DefaultLimit`, `WriteOptions`, выбор комментариев `Comments`, предупреждение `Warning`.
- Вход операций: `IssueInput`, `IssueUpdate`, `ArticleInput`, `ArticleUpdate`, `WorkItemInput`, `WorkItemUpdate`,
  `AttributeWrite`, `File`, `TagSharing`, `TagOptions`; опции `Show…Options`, `List…Options`; наборы полей по умолчанию
  `…Fields`; `ActivityCategories`.
- `fake.ServeUnread` — сервер для теста, обработчик которого читает тело сам, как у запроса, который клиент обрывает.

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
