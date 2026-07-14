[@pbvex/protocol](../index.md) / isPlainObject

# Function: isPlainObject()

> **isPlainObject**(`value`): `value is Record<string, unknown>`

Reports whether a value is an ordinary record, including records created in
another JavaScript realm. Cross-realm Object.prototype identities differ,
so prototype identity alone is insufficient; a genuine Object prototype is
itself rooted at null and owns the Object constructor.

## Parameters

### value

`unknown`

## Returns

`value is Record<string, unknown>`
