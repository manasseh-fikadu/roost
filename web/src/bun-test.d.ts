// Types for `bun test`, which runs the .test.ts files next to the code they
// test. TypeScript 6 only loads the @types packages a project names, so this
// names Bun's. Only tests should use them; the app runs in a browser.
/// <reference types="bun" />
