function hashString(str: string): string {
  let h = 5381;
  for (let i = 0; i < str.length; i++) {
    h = ((h << 5) + h + str.charCodeAt(i)) >>> 0;
  }
  return h.toString(36);
}

export function deriveFunctionName(modulePath: string, exportName: string): string {
  const base = modulePath
    .replace(/\.ts$/, '')
    .replace(/[^a-zA-Z0-9_]/g, '_')
    .replace(/_+/g, '_')
    .replace(/^_|_$/g, '');
  let prefix = base;
  if (!/^[a-zA-Z]/.test(prefix)) {
    prefix = `fn_${prefix}`;
  }
  const hash = hashString(`${modulePath}:${exportName}`);
  let name = `${prefix}_${exportName}_${hash}`;
  if (name.length > 1024) {
    const excess = name.length - 1024;
    prefix = prefix.slice(0, Math.max(0, prefix.length - excess));
    name = `${prefix}_${exportName}_${hash}`;
  }
  if (!/^[a-zA-Z][a-zA-Z0-9_]*$/.test(name) || name.length > 1024) {
    throw new Error(`Could not derive a valid identifier for ${modulePath}#${exportName}`);
  }
  return name;
}

export const DERIVE_FUNCTION_NAME_JS = `function pbvexDeriveFunctionName(modulePath, exportName) {
  function hashString(str) {
    var h = 5381;
    for (var i = 0; i < str.length; i++) {
      h = ((h << 5) + h + str.charCodeAt(i)) >>> 0;
    }
    return h.toString(36);
  }
  var base = modulePath.replace(/\\.ts$/, '').replace(/[^a-zA-Z0-9_]/g, '_').replace(/_+/g, '_').replace(/^_|_$/g, '');
  var prefix = base;
  if (!/^[a-zA-Z]/.test(prefix)) prefix = 'fn_' + prefix;
  var hash = hashString(modulePath + ':' + exportName);
  var name = prefix + '_' + exportName + '_' + hash;
  if (name.length > 1024) {
    var excess = name.length - 1024;
    prefix = prefix.slice(0, Math.max(0, prefix.length - excess));
    name = prefix + '_' + exportName + '_' + hash;
  }
  if (!/^[a-zA-Z][a-zA-Z0-9_]*$/.test(name) || name.length > 1024) {
    throw new Error('Could not derive a valid identifier for ' + modulePath + '#' + exportName);
  }
  return name;
}`;
