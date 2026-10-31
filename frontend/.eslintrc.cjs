module.exports = {
  root: true,
  env: {
    browser: true,
    es2021: true,
    node: true,
  },
  parser: "vue-eslint-parser",
  parserOptions: {
    parser: "@typescript-eslint/parser",
    ecmaVersion: "latest",
    sourceType: "module",
    extraFileExtensions: [".vue"],
  },
  plugins: ["vue", "@typescript-eslint"],
  extends: [
    "eslint:recommended",
    "plugin:vue/vue3-essential",
    "plugin:@typescript-eslint/recommended",
  ],
  rules: {
    "no-constant-condition": "off",
    "no-mixed-spaces-and-tabs": "off",
    "no-useless-escape": "off",
    "no-unused-vars": "off",
    "@typescript-eslint/no-unused-vars": [
      "warn",
      { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
    ],
    "@typescript-eslint/ban-types": "off",
    "@typescript-eslint/ban-ts-comment": "off",
    "@typescript-eslint/no-explicit-any": "off",
    "no-restricted-syntax": [
      "error",
      {
        selector: "CatchClause > *[typeAnnotation.typeAnnotation.type='TSAnyKeyword']",
        message: "Catch errors as unknown and narrow their fields; preserve the original error for cancellation and rethrow.",
      },
      {
        selector: "CallExpression[callee.name='defineEmits'] > ArrayExpression.arguments",
        message: "Declare explicit event payload types instead of an untyped event-name array.",
      },
    ],
    "vue/multi-word-component-names": "off",
    "vue/no-use-v-if-with-v-for": "off",
  },
};
