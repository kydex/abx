# Выпуски и CI для сопровождающего

[English](../releases.md) | **Русский**

Этот документ предназначен для сопровождающего и разработчиков. Обычным пользователям следует использовать [руководство по установке](installation.md); работать с GitHub Actions для установки ABX не требуется.

## Источник версии

У номера выпуска один источник:

```text
internal/app/VERSION
```

Приложение встраивает этот файл, а `scripts/package.sh` читает его при проверке тега и формировании имён артефактов.

## Локальная проверка выпуска

Перед публикацией версии:

```sh
make check
make live
```

`make live` следует запускать на репрезентативном поддерживаемом хосте, если менялось runtime/isolation поведение.

[Необязательные ручные проверки](manual-checks.md) доступны для диагностики или исследования конкретной проблемы. Их не требуется выполнять перед каждым релизом; они не добавляют условий выпуска.

Затем локально проверьте packaging:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
bash scripts/package.sh "$tag"
cd dist
sha256sum -c "abx-$version-SHA256SUMS"
```

Packaging script не запускает тесты и не создаёт git tag. Тег, не совпадающий с `internal/app/VERSION`, отклоняется.

Скрипт создаёт:

| Файл | Содержимое |
|---|---|
| `abx-X.Y.Z-linux-amd64.tar.gz` | Статический Linux amd64 бинарник, пользовательская документация и лицензии |
| `abx-X.Y.Z-source.tar.gz` | Исходники, тесты, документация, Makefile, packaging script и workflows |
| `abx-X.Y.Z-SHA256SUMS` | SHA-256 бинарного и исходного архивов |

## GitHub Actions

CI относится к инфраструктуре разработки и выпуска, а не к пользовательской установке.

- `.github/workflows/check.yml` запускает `make check` для push и pull requests и переиспользуется release-workflow.
- `.github/workflows/live.yml` запускает настоящие Bubblewrap tests на CI runner.
- `.github/workflows/release.yml` запускается для тегов `v*`, требует успеха check и live jobs, выполняет `scripts/package.sh` и загружает `dist/*` как workflow artifact.

Workflow artifact предназначен для проверки и дальнейшей публикации сопровождающим. Текущий workflow **сам не создаёт** страницу GitHub Release. Если проект распространяет бинарники через release page или другой публичный канал, сопровождающий должен опубликовать там проверенные `dist/*`, чтобы пользователь мог следовать обычной инструкции установки и не работать с Actions.

## Публикация версии

1. Перенесите нужные записи changelog из `Unreleased` в новую секцию `X.Y.Z`.
2. Измените `internal/app/VERSION` на то же значение `X.Y.Z`.
3. Выполните `make check`, нужные live tests и локальную проверку packaging/checksums.
4. Закоммитьте и отправьте release-изменения.
5. После успешного CI ветки создайте и отправьте annotated tag:

```sh
version=$(cat internal/app/VERSION)
tag="v$version"
git tag -a "$tag" -m "ABX $version"
git push origin "$tag"
```

6. Убедитесь, что tag workflow прошёл. Скачайте его workflow artifact, проверьте `abx-X.Y.Z-SHA256SUMS` и сделайте smoke-test бинарника.
7. Опубликуйте проверенные бинарный архив, source archive и checksum manifest через пользовательский release-канал проекта.

Не переносите уже опубликованный version tag на другое содержимое. Последующие исправления выпускайте новой версией.

Метаданные архивов не нормализованы для побайтовой воспроизводимости, build attestation текущий workflow не создаёт.
