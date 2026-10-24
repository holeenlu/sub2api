// Reuse the project's translations; run after vendor/common locale changes.
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const ts = require('../../frontend/node_modules/typescript')
const locales = ['zh', 'en', 'zh-TW', 'ja']
const flatten = (value, prefix = '', result = {}) => {
  for (const [key, item] of Object.entries(value)) {
    if (typeof item === 'string') result[prefix + key] = item
    else if (item && typeof item === 'object') flatten(item, prefix + key + '.', result)
  }
  return result
}
const read = (locale, file) => {
  const source = fs.readFileSync(path.join(__dirname, '../../frontend/src/i18n/locales', locale, file + '.ts'), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText
  const scope = { exports: {} }
  vm.runInNewContext(compiled, scope)
  return flatten(scope.exports.default)
}
const translations = {}
for (const file of ['vendor', 'common']) {
  const messages = locales.map(locale => read(locale, file))
  for (const [key, source] of Object.entries(messages[0])) {
    if (!/[\u3400-\u9fff]/.test(source) || /[{}@|]/.test(source) || translations[source]) continue
    if (messages.every(m => typeof m[key] === 'string' && !/[{}@|]/.test(m[key]))) translations[source] = messages.map(m => m[key])
  }
}
const output = '/* Generated from native vendor/common locales. Do not edit by hand. */\n' +
  'globalThis.VendorPrototypeTranslations = ' + JSON.stringify(translations, null, 2) + ';\n'
fs.writeFileSync(path.join(__dirname, 'prototype-translations.js'), output)
console.log('Generated ' + Object.keys(translations).length + ' shared UI translations.')
