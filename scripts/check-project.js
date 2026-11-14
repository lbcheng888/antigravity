#!/usr/bin/env node
'use strict';

/**
 * Offline integrity and structure verification script for the unpacked
 * Antigravity Electron main-process workspace (`dist/`, `src/`, and `build/`).
 *
 * Checks:
 *  1. package.json manifest, entry point (`main`), tsconfig.json, and root icon assets.
 *  2. Presence of every declared dependency inside `node_modules/`.
 *  3. JavaScript syntax and relative `require(...)` resolution across `dist/` and `build/`.
 *  4. 1-to-1 file coverage across `dist/`, `src/`, and `build/`, plus export symbol parity.
 */

const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');
const { builtinModules } = require('module');

const ROOT = path.resolve(__dirname, '..');
const DIST_DIR = path.join(ROOT, 'dist');
const SRC_DIR = path.join(ROOT, 'src');
const BUILD_DIR = path.join(ROOT, 'build');
const BUILTINS = new Set(
  builtinModules.flatMap((m) => [m, m.replace(/^node:/, '')])
);

function walkFiles(dir, ext) {
  const results = [];
  if (!fs.existsSync(dir)) {
    return results;
  }
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      results.push(...walkFiles(fullPath, ext));
    } else if (entry.isFile() && entry.name.endsWith(ext)) {
      results.push(fullPath);
    }
  }
  return results.sort();
}

function collectExports(content) {
  const names = new Set();
  for (const m of content.matchAll(/\bexports\.([A-Za-z0-9_]+)\s*=/g)) {
    if (m[1] !== '__esModule') {
      names.add(m[1]);
    }
  }
  for (const m of content.matchAll(
    /Object\.defineProperty\(\s*exports\s*,\s*['"]([A-Za-z0-9_]+)['"]/g
  )) {
    if (m[1] !== '__esModule') {
      names.add(m[1]);
    }
  }
  return [...names].sort();
}

function verifyJsTree(treeRoot, label) {
  const files = walkFiles(treeRoot, '.js');
  const missingRelative = [];
  const runtimeExternals = new Set();

  for (const filePath of files) {
    execFileSync(process.execPath, ['--check', filePath], { stdio: 'pipe' });

    const content = fs.readFileSync(filePath, 'utf8');
    const requirePattern = /(?:^|[^\w$.])require\(\s*['"]([^'"]+)['"]\s*\)/gm;
    for (const match of content.matchAll(requirePattern)) {
      const spec = match[1];
      if (spec.startsWith('.')) {
        try {
          require.resolve(spec, { paths: [path.dirname(filePath)] });
        } catch {
          missingRelative.push({
            file: path.relative(ROOT, filePath),
            spec,
          });
        }
      } else if (
        !BUILTINS.has(spec) &&
        !BUILTINS.has(spec.replace(/^node:/, ''))
      ) {
        runtimeExternals.add(spec);
      }
    }
  }

  if (missingRelative.length > 0) {
    const details = missingRelative
      .map((item) => `${item.file} -> ${item.spec}`)
      .join('\n');
    throw new Error(`Unresolved relative requires in ${label}:\n${details}`);
  }

  return { files, runtimeExternals };
}

function main() {
  const pkgPath = path.join(ROOT, 'package.json');
  const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));

  const requiredFiles = [
    pkg.main,
    'tsconfig.json',
    'src/types/vitest.d.ts',
    'icon.png',
    'trayTemplate.png',
    'trayTemplate@2x.png',
  ];
  for (const rel of requiredFiles) {
    const full = path.join(ROOT, rel);
    if (!fs.existsSync(full)) {
      throw new Error(`Missing required file: ${rel}`);
    }
  }

  const declaredDeps = Object.keys(pkg.dependencies || {});
  const missingDeps = [];
  for (const dep of declaredDeps) {
    const depPkg = path.join(ROOT, 'node_modules', dep, 'package.json');
    if (!fs.existsSync(depPkg)) {
      missingDeps.push(dep);
    }
  }
  if (missingDeps.length > 0) {
    throw new Error(`Missing declared dependencies: ${missingDeps.join(', ')}`);
  }

  const distCheck = verifyJsTree(DIST_DIR, 'dist');

  const missingSrc = [];
  const missingBuild = [];
  const exportMismatches = [];

  for (const distPath of distCheck.files) {
    const rel = path.relative(DIST_DIR, distPath);
    const tsRel = rel.slice(0, -3) + '.ts';
    const srcPath = path.join(SRC_DIR, tsRel);
    const buildPath = path.join(BUILD_DIR, rel);

    if (!fs.existsSync(srcPath)) {
      missingSrc.push(`src/${tsRel}`);
    }
    if (!fs.existsSync(buildPath)) {
      missingBuild.push(`build/${rel}`);
      continue;
    }

    const expected = collectExports(fs.readFileSync(distPath, 'utf8'));
    const actual = new Set(collectExports(fs.readFileSync(buildPath, 'utf8')));
    const missing = expected.filter((name) => !actual.has(name));
    if (missing.length > 0) {
      exportMismatches.push({ rel, missing });
    }
  }

  if (missingSrc.length > 0) {
    throw new Error(`Missing TypeScript sources:\n${missingSrc.join('\n')}`);
  }
  if (missingBuild.length > 0) {
    throw new Error(
      `Missing compiled build outputs (run "npm run build" first):\n${missingBuild.join('\n')}`
    );
  }
  if (exportMismatches.length > 0) {
    const details = exportMismatches
      .map((m) => `${m.rel}: missing [${m.missing.join(', ')}]`)
      .join('\n');
    throw new Error(`Export mismatches between dist/ and build/:\n${details}`);
  }

  const buildCheck = verifyJsTree(BUILD_DIR, 'build');

  console.log(`Package: ${pkg.name}@${pkg.version} (main: ${pkg.main})`);
  console.log(`Declared dependencies verified: ${declaredDeps.length}`);
  console.log(`dist/*.js verified: ${distCheck.files.length} files`);
  console.log(`src/*.ts coverage verified: ${distCheck.files.length} module files`);
  console.log(
    `build/*.js syntax, relative requires & export parity verified: ${buildCheck.files.length} files`
  );
  console.log(
    `External/runtime modules referenced: ${[...distCheck.runtimeExternals].sort().join(', ')}`
  );
}

main();
