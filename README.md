# go-youtrack

Go SDK для YouTrack. Клиент по адресу инстанса и постоянному токену несёт сервисы по сущностям: задачи, статьи,
комментарии, вложения, связи, теги, записи времени, активности, проекты, кастом-поля и пользователи. Операции
отдают документ `*Node` — дерево ответа под выражением полей вызывающего — или значения Go там, где потребителю
нужны типы: задача с кастом-полями, запись полей, метаданные проекта, бандл поля, пользователи. Ошибка одна —
`*Error` с кодом, который называет, что делать дальше.

```bash
go get github.com/hakastein/go-youtrack@v0.4.0
```

Словарь — [`CONTEXT.md`](CONTEXT.md), решения — [`docs/adr/`](docs/adr/), правила для агента — [`AGENTS.md`](AGENTS.md),
изменения — [`CHANGELOG.md`](CHANGELOG.md).

## Клиент

```go
c, err := youtrack.NewClient("https://youtrack.example.org", os.Getenv("YOUTRACK_TOKEN"))
if err != nil {
	return err
}
issue, err := c.Issues.Get(ctx, "DEV-13271")
```

| Сервис | Документ `*Node` | Значения Go |
|---|---|---|
| `Issues` | `Show`, `List`, `Create`, `Update`, `Delete` | `Get`, `WriteFields` |
| `Articles` | `Show`, `List`, `Children`, `Create`, `Update`, `Delete` | |
| `Comments` | `List`, `Create`, `Update`, `Delete` | |
| `Attachments` | `List`, `Create`, `Delete` | |
| `Links` | `List`, `Add`, `Remove` | |
| `Tags` | `List`, `Create`, `Delete`, `Add`, `Remove` | |
| `WorkItems` | `List`, `Create`, `Update`, `Delete` | |
| `Activities` | `List` | |
| `Projects` | `Show`, `List` | |
| `Fields` | `Show`, `List` | `Metadata`, `ReadMetadata`, `Bundle` |
| `Users` | `Show`, `List` | `Me`, `Find` |

Контекст идёт первым аргументом, за ним то, что операция адресует (id задачи, код проекта, фраза связи), вход записи
и опции указателем на структуру: `nil` — умолчания, но выражение полей у документной операции обязательно. Значением вход берут только `Tags.Create` (`TagSharing`),
`Attachments.Create` (`File`) и `Users.Find` (`limit int`). Предупреждения поиска задач доставляет функция `Warn` в
опциях.
Вызов, который нельзя отправить как написан, отвергается `bad_usage` до записи: чтения, которые нужны для проверки
(метаданные проекта, задача, цель связи), к этому моменту могли пройти.

Процессу на один вызов, как CLI, `youtrack.WithMetadataCache(dir)` даёт кэш метаданных проекта и каталога кастом-полей
инстанса на диске: каталог на пару адреса и токена (`0700`), файл на проект и файл на каталог полей (`0600`), токена в
файлах нет. `Fields.Metadata` отвечает из кэша (`FromCache`), `Fields.ReadMetadata` читает сервер и кладёт в кэш,
`Fields.Show` и `Fields.Bundle` читают проект снова, когда кэш разошёлся с сервером. Операции задачи разрешают имена
кастом-полей по кэшированному каталогу и читают его снова, когда имени в нём нет. Долгоживущему процессу кэш не нужен.

## Документ

Документная операция отвечает отображением ключей в порядке
выражения полей, списки, строки, числа как их прислал сервер, многострочный текст (`description`, `text`,
`content`). Все решения о данных приняты при построении узла, поэтому печать — только байты по аксессорам
узла, а `json.Marshal` пишет JSON в том же порядке.

```go
doc, err := c.Issues.Show(ctx, "DEV-1", &youtrack.ShowIssueOptions{
	Fields:   `idReadable,summary,customFields("Модуль системы"),attachments(name,url)`,
	Comments: youtrack.LastComments(3),
})
summary, _ := doc.Lookup("summary")
fmt.Println(summary.Value())
```

| Узел | `Kind` | `Value` |
|---|---|---|
| `NewNull()` | `NullNode` | `""` |
| `NewString(s)` | `StringNode` | `s` |
| `NewNumber(n)` | `NumberNode` | число, как его написал сервер |
| `NewBool(b)` | `BoolNode` | `true` или `false` |
| `NewText(s)` | `TextNode` | многострочный текст |
| `NewList(items...)` | `ListNode` | `""`; элементы — `Items()` |
| `NewMap(pairs...)` | `MapNode` | `""`; пары — `Pairs()`, значение по ключу — `Lookup(key)` |

Ключ пары — имя модуля (грамматика `CheckKey`: ASCII-буквы, цифры, `_` и `$`, не с цифры, не bool и не null для
YAML 1.1) или имя из данных (`DataPair`, `FromData`): имя кастом-поля, фраза связи.

- **Выражение полей** (`Fields`): `fields=` REST API, ровно то, что прочитать; пустое — `bad_usage`, своих полей по
  умолчанию у модуля нет. Кастом-поле задачи называется под `customFields`: без кавычек, если в имени только буквы и
  цифры любого алфавита, `_` и `$`, иначе в кавычках — `customFields(State,Категория,"Статус разработки")`. Каждое
  такое имя сверяется с каталогом кастом-полей инстанса. Имя, которого нет в схеме `$type`, —
  `unknown_name` с ближайшими именами; запрошенное и не пришедшее — `upstream_invalid` с `missing`.
- **Страница** (`Page{Skip, Limit}`): `Limit` 0 — `DefaultLimit` (50). Документ списка несёт `total`, `returned`,
  `truncated` и записи; счётчик, который сервер не назвал, — `null`.
- **Запись** отвечает документом записанного (`WriteOptions.Fields`), и модуль сверяет ответ с отправленным: расхождение
  после записи — `upstream_invalid` с `mismatch` и `AfterWrite`.
- **Предупреждение** (`ListIssuesOptions.Warn`): свободный текст в запросе поиска, который YouTrack ищет словами, а не
  читает условием, — `*Warning` с `query` и `free_text`.

## Задача и кастом-поля значениями Go

```go
issue, err := c.Issues.Get(ctx, "DEV-13271")
if module, ok := issue.Field("Модуль системы"); ok {
	for _, v := range module.Values {
		fmt.Println(v.ID, v.Text, v.LocalizedName)
	}
}
for _, link := range issue.Links {
	if link.Type.Name == "Subtask" && link.Direction == youtrack.Inward && len(link.Issues) > 0 {
		fmt.Println("parent:", link.Issues[0].IDReadable)
	}
}

issue, err = c.Issues.WriteFields(ctx, "DEV-13271", []youtrack.FieldWrite{
	{Name: "Тестирование", Values: []string{"Пройдено"}},
	{Name: "Модуль системы", Values: []string{"Инфраструктура. DevOps", "TMS"}},
	{Name: "Assignee", Clear: true},
})

bundle, err := c.Fields.Bundle(ctx, "DEV", "Модуль системы")
users, err := c.Users.Find(ctx, "Колесов", 10)
```

`Issues.Create`, `Issues.Update` и `Issues.WriteFields` пишут кастом-поля одним путём: имена разрешаются по
метаданным проекта (по имени, затем по переводу, без учёта регистра), класс берётся с задачи, если поле на ней
стоит, иначе из таблицы типов, обязательные поля проверяются до отправки (при обновлении — только опустошаемые),
скрытые условием — при создании, ответ сверяется с записанным. `WriteFields` отвечает `*Issue` вместо документа.

Значение кастом-поля читается и пишется по ключу значения его типа. `Value.Text` — ключ значения текстом,
`Value.ID` — внутренний id у значения бандла, пользователя и группы, `Value.LocalizedName` — перевод, который
интерфейс показывает у значения бандла: `State` хранит `In Progress` и показывает «В работе».

| `valueType` | `Value.Text` и элемент `FieldWrite.Values` | Тело записи |
|---|---|---|
| `enum`, `state`, `version`, `build`, `ownedField`, `group` | `name` | `{"name": …}` |
| `user` | `login` | `{"login": …}` |
| `period` | период ISO 8601 из минут: `PT1H30M`, `PT0M` | `{"minutes": N}` |
| `date` | день ISO 8601: `2026-09-16` | полдень UTC этого дня в миллисекундах |
| `date and time` | момент ISO 8601 в UTC; на вход — с любым смещением | миллисекунды с эпохи |
| `integer`, `float` | число в самой короткой десятичной записи | число |
| `string` | сама строка | строка |
| `text` | сам текст | `{"text": …}` |

Значение поля вне двадцати пар `valueType` и `isMultiValue` модуль читать и писать отказывается (`upstream_invalid`);
`FieldType.Known` говорит, знает ли модуль тип. Потребителю, который собирает запросы сам, знание о полях доступно
примитивами: `FieldType.Class`, `Encode`, `ReadValue`, `Named`, `Same` и `ValueKeys`.

## Ошибки

Каждая ошибка модуля — `*Error`: `Code`, `Message`, `Details` (пары узла: `request`, `upstream_status`,
`upstream_error`, `upstream_message`, `upstream_body`, `unknown`, `missing`, `invalid`, `mismatch` и другие),
`AfterWrite` и `Err` — ошибка транспорта под запросом без ответа или с оборванным телом ответа. `errors.Is` сравнивает по коду с сентинелом.

```go
_, err := c.Issues.Update(ctx, "DEV-1", &youtrack.IssueUpdate{Summary: &summary}, nil)
var failed *youtrack.Error
switch {
case errors.Is(err, youtrack.ErrNotFound):
	// нет такой задачи
case errors.As(err, &failed) && failed.MayHaveWritten():
	// инстанс мог измениться: повторять ли запись, решает вызывающий
}
```

| Код | Сентинел | Когда | Что дальше |
|---|---|---|---|
| `bad_usage` | `ErrBadUsage` | вызов нельзя отправить как написан, в том числе по метаданным проекта | чинить вызов; инстанс не изменился |
| `unknown_name` | `ErrUnknownName` | имя не нашлось там, где его искали | чинить имя по `nearest` или `candidates` |
| `missing_required` | `ErrMissingRequired` | запись оставляет пустыми обязательные поля проекта | заполнить все названные |
| `not_found` | `ErrNotFound` | сервер ответил `404` | проверить идентификатор |
| `denied` | `ErrDenied` | `401`/`403` или проект без единого видимого поля | токен и его права |
| `rejected` | `ErrRejected` | сервер отклонил запрос с `400` | читать `upstream_*`; ничего не записано |
| `upstream_failed` | `ErrUpstreamFailed` | `5xx` YouTrack, сбой соединения, таймаут, статус вне таблицы | повтор может помочь |
| `upstream_invalid` | `ErrUpstreamInvalid` | ответ не сходится с запросом | повтор не поможет |
| `write_uncertain` | `ErrWriteUncertain` | запись ушла, ответа о её исходе нет | решает вызывающий |

`MayHaveWritten()` — `write_uncertain` или `AfterWrite`. Слова сервера передаются
дословно под `upstream_*`, прозу модуля вызывающий не разбирает.

## Транспорт и API

Транспорт отправляет каждый запрос один раз: HTTP/1.1, keep-alive выключен, редиректы не выполняются, прокси не
читается, модуль не повторяет ни чтение, ни запись. Всё, чего сервисы не покрывают, доступно через `c.API()` —
сгенерированный клиент `ytapi.Client` ко всем операциям REST API поверх того же транспорта и токена. Тело ответа он
отдаёт как `*http.Response`; запись через него отправляется `youtrack.Send`, который отличает неотправленный
запрос (`upstream_failed`) от запроса с неизвестным исходом (`write_uncertain`).

## Тесты

Пакет `fake` — фейковый сервер YouTrack: один `httptest.Server` на тестовый пакет, префикс пути на тест, журнал
запросов с `Paths`, `Fields`, `Bodies` и `Last`, сборщики ответов `JSON` и `InTurn`, `ServeNothing` для вызова,
который не должен дойти до сети, `Unreachable` для адреса, на котором никто не слушает, `ServeAlone` для теста,
которому надо перестать слушать между двумя запросами, и `ServeUnread` для обработчика, который читает тело сам. Тесты
модуля — контрактные, через экспорт и против этого сервера; тесты потребителей ходят к нему же.

```bash
make go        # gofmt, go vet, go test -race
make generate  # ytapi и каталог схем из спецификации
make ytapi     # сгенерированное воспроизводится из спецификации и overlay байт в байт
make openapi   # снять спецификацию с дев-инстанса YouTrack 2026.1.13757 (YOUTRACK_URL)
```

## Версии

Теги semver. До `v1.0.0` минорная версия может менять API; патч — нет. Факты о сервере измерены на YouTrack
2026.1.13757 и записаны в ADR вместе с версией.
