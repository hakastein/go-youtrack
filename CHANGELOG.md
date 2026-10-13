# Изменения

## v0.10.0

Ломающие:

- Звезда пользователя в `IssueInput.Tags`, `Tags.Add` и `Tags.Remove` у задачи — `bad_usage` до записи: сервер держит
  её отдельно от тегов задачи, и запись звезды тегом не сверить.
- Буквальный повтор тега в `IssueInput.Tags` (то же имя и тот же владелец) — `bad_usage` до запроса.

Новое:

- `IssueInput.Star` и `IssueUpdate.Star` пишут звезду задачи (`watchers.hasStar`) и сверяют её в ответе записи.
  `nil` в `IssueInput.Star` оставляет звезду профилю пользователя. `false` снимает звезду, которую профиль поставил
  на новую задачу, второй записью; сбой второй записи — ошибка после записи с id созданной задачи.
- Звезда пользователя читается из профиля (`/api/users/me/profiles/general`) и лежит в кэше метаданных сутки.

## v0.9.0

Новое:

- `IssueInput.Tags` вешает теги в том же `POST /api/issues`, что создаёт задачу. Тег называется `TagRef` — именем и
  необязательным `OwnedBy`, как у `Tags.Add`. Имена разрешаются до записи: имя, которого нет среди тегов, видимых
  токену, и неоднозначное имя — `unknown_name`, и на инстансе ничего не меняется. Ответ без записанного тега —
  `upstream_invalid` с `mismatch` после записи.
- Два имени, которые разрешились в один тег, — `bad_usage` до записи.
- Отказ сервера на создание с тегами (`denied`, `rejected`) несёт в `tags` все отправленные теги: имя, логин
  владельца и id. Сервер не говорит, какой тег отказал.
- `Tags.Add`, `Tags.Remove` и `Tags.Delete` разрешают имя тем же путём; код и детали их ошибок прежние.

## v0.8.0

Ломающие:

- `Activities.List` принимает `Owner`: история задачи или статьи.
- `ListActivitiesOptions.Categories` — `[]ActivityCategory`; `ActivityCategories()` заменён на
  `IssueActivityCategories()`, категории — константы `IssueActivityCategory`.
- Категория другого вида владельца — `unknown_name`, `nil` среди категорий — `bad_usage`.

Новое:

- История статьи: `ArticleActivityCategories()` и константы `ArticleActivityCategory`.
- Объект с `$type`, которого спецификация не объявляет, читается по схеме своего свойства.

## v0.7.0

Ломающие:

- Читаемый id — тип: `IssueID` у `Issues.Show`, `Issues.Get`, `Issues.Update`, `Issues.WriteFields`,
  `Issues.Delete`, `Links`, `WorkItems` и `Activities.List`; `ArticleID` у `Articles.Show`, `Articles.Children`,
  `Articles.Update`, `Articles.Delete` и в `ArticleInput.Parent`, `ArticleUpdate.Parent`; `Owner` у `Comments`,
  `Attachments`, `Tags.Add` и `Tags.Remove`. Литерал `"DEV-1"` подходит туда, где ждут `IssueID` или `ArticleID`, а
  владельца называет только типизированное значение.
- `IssueID` с id статьи, `ArticleID` с id задачи и `nil` вместо владельца — `bad_usage` до запроса.

Новое:

- `OwnerOf` читает вид владельца по форме id: `DEV-1` — `IssueID`, `DEV-A-1` — `ArticleID`.

## v0.6.0

Ломающие:

- Адрес с пользователем или паролем (`https://user:pass@…`) — `bad_usage`. Пароль из адреса и раньше не уходил на
  сервер: клиент шлёт `Authorization: Bearer`, и `net/http` userinfo не использует.
- Адрес с пустым фрагментом (`https://yt.example.org/#`) — `bad_usage`, как адрес с `?`.
- Отказ в адресе — одно сообщение на все случаи, адрес в нём не повторяется.

Новое:

- `ParseAddress` и `CheckToken` — правила адреса и токена `NewClient`, чтобы вызывающий проверял их до создания
  клиента.

## v0.5.1

Исправлено:

- Имя кастом-поля под `customFields` задачи без кавычек — буквы и цифры любого алфавита, `_` и `$`:
  `customFields(Категория)` читает поле, а не отвергается до запроса. Грамматика `CheckKey` проверяет только имена
  модуля, поэтому `customFields(1C)` и `customFields(Yes)` тоже читают поле.

## v0.5.0

Своих полей по умолчанию у операций с `Fields` нет: выражение полей всегда пишет вызывающий.

Ломающие:

- Выражение полей — `fields=` REST API как есть: `+x` к полям по умолчанию удалён, а пустое выражение, в том числе
  при `nil` вместо опций, — `bad_usage` до запроса.
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
