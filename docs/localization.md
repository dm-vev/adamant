# Command description localization

Built-in commands use stable keys such as `commands.about.description`. The
catalogs live in `server/cmd/builtin/locales/` and are embedded in the executable.

`en.json` defines every command's English description and is the single source
for console fallbacks. Other language files can contain just the keys they
translate. The `server/internal/i18n` loader is independent of commands and can
be reused for other server text.

## Adding a language

Add a UTF-8 JSON file named after a BCP 47 language tag, for example `fr.json`:

```json
{
  "commands.about.description": "Affiche les informations sur le noyau Adamant et sa compilation.",
  "commands.banlist.description": "Affiche les joueurs ou les adresses IP bannis."
}
```

Use the same keys as `en.json`. No Go registration, language switch or dependency
change is needed. Run `go test ./server/internal/i18n ./server/cmd/builtin`, then
rebuild and restart the server to include the new catalog.

Use a base language file such as `fr.json` for shared text. Regional files such
as `fr-CA.json` can override individual entries. Underscore filenames such as
`fr_CA.json` are also accepted, but do not create both spellings of one locale.

Each message is looked up independently: exact locale, its CLDR parent locales,
then English. For example, a player using `fr-CA` gets Canadian overrides, then
shared French text, then English for any remaining keys. An unsupported language
uses English. Omit untranslated keys; an empty value is an error.

The loader rejects malformed JSON, invalid UTF-8, duplicate locales or keys,
blank messages, and translation keys missing from `en.json`. Tests load the
embedded catalogs, so these errors fail validation before a release.

## Native Bedrock descriptions

Commands with existing Bedrock descriptions continue to send the native key in
`AvailableCommands`. The client supplies the translation in its current language,
including translations provided by its resource packs. The mapping in
`server/cmd/builtin/description.go` accounts for names that differ between Adamant
and BDS. It contains identifiers, not translated prose.

For these commands, `en.json` supplies only the server/console fallback. Adding a
server translation for a native description does not override the client's text.
The keys in `ru.json` identify the descriptions currently requiring server-side
translation; these are the useful starting point for another language.

## Adding a command

Add `commands.<canonical-name>.description` to `en.json` and use `newCommand`
without a hard-coded description. Add that key to other catalogs as translations
become available. If the Bedrock client already has a suitable native key, add
the verified mapping to `nativeCommandDescriptions` instead of copying vanilla
translations into server catalogs.

Description values are literal strings, without format arguments or plural
rules. Command names, aliases, enum values and syntax tokens are not translated.
Server `/help` preserves the translated descriptions in command output; console
and log output use their English fallbacks.
