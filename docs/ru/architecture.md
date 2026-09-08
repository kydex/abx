# Архитектура

[English](../architecture.md) | **Русский**

Этот документ предназначен для разработчиков. Пользовательское поведение описано в [Использовании](usage.md), [Установке](installation.md) и [Модели защиты](security.md).

Runtime ABX намеренно остаётся небольшим: host resources выбираются и удерживаются, затем строится и проверяется sandbox plan, после чего один backend Bubblewrap выполняет этот план.

## Пакеты

| Пакет | Ответственность |
|---|---|
| `cmd/abx` | Точка входа процесса и стандартные потоки |
| `internal/cli` | Разбор публичных аргументов CLI без чтения host state |
| `internal/profile` | Проверка доменных правил имени профиля, общая для CLI и host-кода |
| `internal/app` | Оркестрация команд, время жизни ресурсов, диагностика, inspect и verify |
| `internal/host` | Account/path resolution, профили, policy проекта, выбор host sources, retained descriptors и revalidation |
| `internal/sandbox` | Чистое окружение, mounts, команды, построение плана и его структурная валидация |
| `internal/bwrap` | Проверка фиксированного Bubblewrap, перевод плана в argv/FD, выполнение, передача сигналов и ожидание |
| `internal/probe` | Наблюдения внутри песочницы для `verify` |

Главная граница:

```text
host state -> host.Inputs -> sandbox.Plan -> bwrap translation/execution
```

`host` решает, какие объекты хоста допустимы. `sandbox` решает, как допустимые объекты видны внутри песочницы. `bwrap` реализует этот план и не должен самостоятельно добавлять новую policy.

## Обычный flow `run` / `shell` / `work`

1. `app` отклоняет запуск от root, затем `cli` разбирает команду.
2. `host` определяет passwd-account, хранилище ABX, профиль, системные источники, optional shared skills и shell. Для `run` и `work` также выбирается текущий проект; одноимённый executable агента требуется только для `run`.
3. Чувствительные host sources открываются и удерживаются внутри `host.Inputs`.
4. `sandbox.Session` строит чистое окружение; `sandbox.Build` создаёт `Plan` и проверяет его структурные invariants.
5. `app` открывает и проверяет `/usr/bin/bwrap` и также удерживает этот executable.
6. Выполняется необходимая подготовка optional mountpoints, затем выбранные host sources повторно проверяются.
7. `bwrap.Translate` ещё раз вызывает валидацию `Plan`, сопоставляет retained sources с `--ro-bind-fd` / `--bind-fd` и строит остальные аргументы Bubblewrap.
8. Bubblewrap запускается через retained descriptor. Стандартные потоки подключаются напрямую; INT/TERM/HUP передаются дочернему процессу.
9. `app` закрывает принадлежащие ему ресурсы. Ошибка cleanup не скрывается успешным завершением child process.

`run` подключает проект и запускает агента. `shell` не подключает проект и запускает passwd login shell. `work` подключает проект и запускает тот же login shell, начиная в `/workspace`. Остальная isolation policy строится общим кодом. Явный режим хранится в `host.Inputs`; `app` преобразует его в режим `sandbox.Input` и выбирает согласованный `PWD`. `Plan` сохраняет отдельный `planWork`, проверяющий ожидаемые источники, mounts и cwd; выбор shell argv проверяется в host/app-тестах.

Выбор shared skills и подготовка mountpoint находятся в `internal/host/skills.go`; владение дескрипторами остаётся в `host.Inputs`. Структурная валидация плана находится в `internal/sandbox/validate.go` и по-прежнему выполняется после построения плана и перед переводом в аргументы Bubblewrap.

## `inspect`

`inspect` выбирает обычные run inputs и строит тот же `Plan`, после чего форматирует его без открытия Bubblewrap и подготовки mountpoints. Filesystem summary выводится из самого плана. Текст о namespaces/network покрыт regression-тестом относительно реальных flags Bubblewrap translation.

`inspect` по-прежнему описывает только `run` и требует executable профиля. Он описывает намерение запуска и не доказывает, что хост сможет создать песочницу.

## `verify`

`verify` выбирает профиль и проект без требования установленного агента. Используются те же plan builder и Bubblewrap backend; дополнительно подключается только фиксированный read-only executable самого ABX как `/abx-probe`.

Probe возвращает наблюдения изнутри песочницы. `app` также владеет временными host-side sentinel files и их cleanup. Финальное сообщение об успехе печатается только после очистки этих ресурсов.

## Правила архитектуры

- Публичный синтаксис держите в `cli`, eligibility host-объектов — в `host`, sandbox visibility/environment — в `sandbox`, запуск процессов — в `bwrap`.
- Владение файлами и процессами должно быть явным; закрывает ресурс слой-владелец.
- Не заменяйте retained-FD mounts повторным pathname lookup после проверки.
- Рассматривайте `Plan.Validate()` как финальную структурную границу перед переводом в backend.
- Добавляйте abstractions только для конкретной проблемы; не создавайте package/interface layers, которые лишь переименовывают существующие операции.
- При изменении поведения синхронно обновляйте английскую и русскую документацию.
