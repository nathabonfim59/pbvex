[@pbvex/protocol](../index.md) / isValidValidatorDescriptor

# Function: isValidValidatorDescriptor()

> **isValidValidatorDescriptor**(`value`): `boolean`

Validates a PBVex validator descriptor. Mirrors the Go schema validator
(schema.ValidateDescriptor) so the TS and backend layers enforce one
deployable, executable descriptor contract: the same types, keys, and
wire-encoded default/literal values. Recursive types use
`{type:'recursive', name, validator}` with `{type:'ref', name}` cycle points;
refs must resolve to a name declared by an enclosing recursive. `delayed` is
never deployable (its closure has no executable descriptor).

## Parameters

### value

`unknown`

## Returns

`boolean`
