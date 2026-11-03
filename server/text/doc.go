// Package text models the text of the game as structured text, which the
// adapter of each protocol renders in its own way: the Bedrock Edition
// adapter to strings with § codes (protocol/bedrock/text), the Java Edition
// adapter to text components (protocol/java/text). The game builds text
// with this package and does not format it for a protocol itself; the
// console renders it with ANSI codes.
//
// # The model
//
// A Text is a component, as in Java Edition: content of its own, a Style,
// and children, which follow the content and inherit the style. The
// content is literal text (Literal), a translation (Translate), whose key
// a client looks up in its own language and fills in with arguments, which
// are texts too, or nothing, as for a text that only groups and styles its
// children (Concat). A Style has a Colour, decorations turned on or off,
// and Reset. A child inherits the style in effect for its parent, and its
// own style overrides it: a colour replaces the colour inherited, and
// decorations it turns on or off are on or off within it. A text with
// Reset inherits no style, not even that of where it is shown, which is
// what §r does in a legacy string.
//
// Colours are the sixteen colours both editions have, the colours of
// materials only Bedrock Edition has (named colours that carry their RGB
// value, which a renderer shows as that value where it has no name for
// them), and any RGB value. The decorations are bold, italic, underlined,
// struck through and obfuscated; Bedrock Edition has no underlined or
// struck through text.
//
// Texts are immutable values. The zero Text is empty, and texts are
// compared with Equal, which compares how they are built, or Equivalent,
// which compares how they look.
//
// # Markup and legacy strings
//
// Colourf builds text from the markup the game has always written, such
// as `<red>Hello <bold>World</bold>!</red>`, with exactly the meaning the
// markup had. Rendered by the Bedrock Edition renderer, the text is byte
// for byte the string the Colourf of the net/bedrock/text package gives for
// the same markup.
//
// The rule for legacy strings: a string that carries § codes, as the names
// and lore of items, the text of signs and books and the messages of
// plugins do today, is read with the meanings Bedrock Edition gives its
// codes, by ParseLegacy. So §m and §n are the colours of redstone and
// copper, not struck through and underlined as in Java Edition, §g to §v
// are colours of materials, and a colour code does not turn decorations
// off. The codes are § and one of 0 to 9 and a to v in lower case; § with
// any other character is text, as the game has always treated it. Literal
// text and Colourf read no § codes: codes in them stay part of the text, so
// strings with codes go through ParseLegacy.
//
// ParseLegacy keeps every code and every reset of a string where it is,
// even those that change nothing, so the Bedrock Edition renderer gives
// back every string it reads, byte for byte. Every string is in canonical
// form in that sense: there is no string ParseLegacy and the renderer
// change.
//
// # Streams of codes
//
// Spans writes a text as a stream of formatting codes and text, which is
// how a legacy string or a terminal shows formatting. Both the Bedrock
// Edition renderer and ANSI write text so, each in its own codes, so the
// rules of when codes and resets are written are in one place.
package text
