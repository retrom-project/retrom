type JsonObject = Record<string, unknown>;

export function parseCanonicalJSON(source: string): unknown {
  let offset = 0;
  const fail = (): never => {throw new SyntaxError("CANONICAL_JSON_INVALID");};
  const peek = () => source[offset];
  const whitespace = () => {
    while ([" ", "\t", "\r", "\n"].includes(peek() ?? "")) {offset += 1;}
  };
  const word = (expected: string, value: unknown) => {
    if (source.slice(offset, offset + expected.length) !== expected) {fail();}
    offset += expected.length;
    return value;
  };
  const hexUnit = () => {
    const digits = source.slice(offset, offset + 4);
    if (!/^[0-9A-Fa-f]{4}$/u.test(digits)) {fail();}
    offset += 4;
    return Number.parseInt(digits, 16);
  };
  const escapedStringUnit = () => {
    offset += 1;
    const escaped = source[offset];
    const simple: Record<string, string> = {
      '"': '"', "\\": "\\", "/": "/", b: "\b", f: "\f", n: "\n", r: "\r", t: "\t",
    };
    if (escaped !== "u") {
      if (!(escaped && Object.hasOwn(simple, escaped))) {fail();}
      offset += 1;
      return simple[escaped];
    }
    offset += 1;
    const first = hexUnit();
    if (first >= 0xdc00 && first <= 0xdfff) {return fail();}
    if (first < 0xd800 || first > 0xdbff) {return String.fromCharCode(first);}
    if (source.slice(offset, offset + 2) !== "\\u") {return fail();}
    offset += 2;
    const second = hexUnit();
    if (second < 0xdc00 || second > 0xdfff) {return fail();}
    return String.fromCodePoint(0x10000 + (first - 0xd800) * 0x400 + second - 0xdc00);
  };
  const string = () => {
    if (source[offset] !== '"') {fail();}
    offset += 1;
    let result = "";
    while (offset < source.length) {
      const character = source[offset];
      const code = source.charCodeAt(offset);
      if (character === '"') {offset += 1; return result;}
      if (character === "\\") {
        result += escapedStringUnit();
        continue;
      }
      if (code < 0x20 || code >= 0xdc00 && code <= 0xdfff) {fail();}
      if (code >= 0xd800 && code <= 0xdbff) {
        const second = source.charCodeAt(offset + 1);
        if (second < 0xdc00 || second > 0xdfff) {fail();}
        result += character + source[offset + 1];
        offset += 2;
      } else {
        result += character;
        offset += 1;
      }
    }
    return fail();
  };
  const number = () => {
    const start = offset;
    if (peek() === "-") {offset += 1;}
    if (peek() === "0") {
      offset += 1;
      if (/\d/u.test(peek() ?? "")) {fail();}
    } else {
      if (!/[1-9]/u.test(peek() ?? "")) {fail();}
      while (/\d/u.test(peek() ?? "")) {offset += 1;}
    }
    const value = Number(source.slice(start, offset));
    if (!Number.isSafeInteger(value)) {fail();}
    return value;
  };
  const array = () => {
    offset += 1;
    whitespace();
    const result: unknown[] = [];
    if (peek() === "]") {offset += 1; return result;}
    while (true) {
      result.push(value());
      whitespace();
      if (peek() === "]") {offset += 1; return result;}
      if (peek() !== ",") {fail();}
      offset += 1;
      whitespace();
    }
  };
  const object = () => {
    offset += 1;
    whitespace();
    const result: JsonObject = {};
    if (peek() === "}") {offset += 1; return result;}
    while (true) {
      const key = string();
      if (Object.hasOwn(result, key)) {fail();}
      whitespace();
      if (peek() !== ":") {fail();}
      offset += 1;
      whitespace();
      result[key] = value();
      whitespace();
      if (peek() === "}") {offset += 1; return result;}
      if (peek() !== ",") {fail();}
      offset += 1;
      whitespace();
    }
  };
  const value = (): unknown => {
    whitespace();
    const character = peek();
    if (character === '"') {return string();}
    if (character === "{") {return object();}
    if (character === "[") {return array();}
    if (character === "t") {return word("true", true);}
    if (character === "f") {return word("false", false);}
    if (character === "n") {return word("null", null);}
    return number();
  };

  const result = value();
  whitespace();
  if (offset !== source.length) {fail();}
  return result;
}
