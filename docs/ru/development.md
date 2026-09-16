# Разработка

[English](../development.md) | **Русский**

Этот документ предназначен для разработчиков. Для обычного использования ABX эти команды не нужны.

## Сборка и тесты

Используйте Linux и версию Go, указанную в `go.mod`.

Быстрые тесты:

```sh
make test
```

Локальная статическая сборка:

```sh
make build
./abx version
```

Полный набор локальных проверок:

```sh
make check
```

Для `make check` команды `shellcheck`, `govulncheck` и `gosec` должны быть доступны в PATH. Сейчас проверка включает:

- проверку `gofmt`;
- ShellCheck для `scripts/*.sh`;
- `govulncheck`;
- `gosec`;
- проверку статической production-сборки;
- Go-тесты с race detector и общим coverage floor из `Makefile`;
- `go vet`.

Race detector требует поддерживаемую Go-платформу и рабочий C toolchain. Во время обычной разработки `make test` быстрее.

## Live-тесты песочницы

На Linux-хосте от обычного пользователя с Bubblewrap 0.12.0+ по пути `/usr/bin/bwrap` и работающими непривилегированными user namespaces:

```sh
make live
```

Эквивалент:

```sh
env ABX_LIVE=1 go test -count=1 ./...
```

Live tests запускают настоящие сессии Bubblewrap и проверяют runtime-поведение, которое обычные unit tests не могут доказать. Запуск тестов от root не заменяет live-run от обычного пользователя.

Для одного сценария:

```sh
env ABX_LIVE=1 go test -count=1 -v ./internal/app -run '^TestLiveVerify$'
```

Live tests нужны после изменений mounts, namespaces, retained descriptors, project/profile path policy, Bubblewrap translation/execution или verify.

Для диагностики с временными профилями при необходимости используйте [необязательные ручные проверки](manual-checks.md). Они не нужны перед каждым релизом и не заменяют описанные выше автоматические или live-тесты.

## Правила разработки

- Начинайте с ожидаемого пользовательского поведения и его security boundary.
- Оставляйте решение в пакете-владельце: CLI syntax в `cli`, eligibility host-объектов в `host`, sandbox policy в `sandbox`, execution в `bwrap`.
- Сохраняйте явное владение ресурсами и обработку ошибок.
- Не ослабляйте path/FD checks только ради хоста, на котором отсутствует требуемая Linux-функциональность.
- Добавляйте regression tests для существенных failure modes и invariants, а не копию каждой детали реализации.
- Синхронно обновляйте `README.md` / `README.ru.md` и каждую пару английского/русского руководства.
- `CHANGELOG.md` ведите на английском.

Workflow выпуска и детали CI описаны в [Выпусках и CI для сопровождающего](releases.md).
