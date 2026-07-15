[@pbvex/protocol](../index.md) / parseContentType

# Function: parseContentType()

> **parseContentType**(`value`): [`ParsedContentType`](../interfaces/ParsedContentType.md) \| `undefined`

Parse a Content-Type header as a media type per RFC 7231/7230:
`type "/" subtype *( OWS ";" OWS parameter )`
where type and subtype are tokens and parameter values are tokens or
quoted-strings. Parameter names are normalized to lowercase.

## Parameters

### value

`string`

## Returns

[`ParsedContentType`](../interfaces/ParsedContentType.md) \| `undefined`
