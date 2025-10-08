#!/usr/bin/env node

/**
 * Update GitHub Actions in .github/workflows/*.ya?ml to the latest suitable release:
 * - Finds lines with `uses: owner/repo[/path]@ref`
 * - Queries GitHub Releases (first page only) for `owner/repo`
 * - Picks the most advanced semver whose major >= currently-installed major (if known)
 * - Resolves the selected release tag to a commit SHA
 * - Replaces the ref with the commit SHA and appends `# vX[.Y[.Z]]` comment
 * - Handles release names/tags like `codeql-bundle-v2.23.2` by extracting `v2.23.2`.
 * - No third-party modules; Node.js 22 standard library only.
 *
 * Notes:
 * - Supports optional GITHUB_TOKEN to raise rate limit: grant classic `public_repo` permission
 */

const fs = require('fs');
const fsp = require('fs/promises');
const path = require('path');
const https = require('https');

const WORKFLOWS_DIR = path.join(process.cwd(), '.github', 'workflows');
const RELEASES_PER_PAGE = 100;

const TOKEN = process.env.GITHUB_TOKEN || null;

// Simple cache maps
const releasesCache = new Map(); // key: 'owner/repo' => array of release objects with extracted versions
const commitShaCache = new Map(); // key: 'owner/repo@tag' => sha

async function main() {
  const files = await findWorkflowFiles(WORKFLOWS_DIR);
  if (files.length === 0) {
    console.log('No workflow files found.');
    return;
  }

  let totalUpdates = 0;

  for (const file of files) {
    const original = await fsp.readFile(file, 'utf8');
    const lines = original.split(/\r?\n/);

    let updated = false;
    for (let i = 0; i < lines.length; i++) {
      const parsed = parseUsesLine(lines[i]);
      if (!parsed) continue;

      // Ignore local or docker actions
      const value = parsed.valueStr;
      if (!value || !value.includes('@')) continue;
      if (value.startsWith('.') || value.startsWith('docker://')) continue;

      const { beforeAt, ref, baseRepo, subPath, quote } = parseUsesValue(value);
      if (!baseRepo) continue;
      console.log(`${file}:${i + 1} checking ${beforeAt}...`)

      try {
        const currentVersion = extractVersionFromCommentOrRef(parsed.comment, ref);
        const releases = await getReleasesForRepo(baseRepo);
        if (!releases || releases.length === 0) continue;

        const candidate = pickBestRelease(releases, currentVersion);
        if (!candidate) continue;

        const tag = candidate.tag;
        const sha = await getCommitShaForTag(baseRepo, tag);

        if (!sha) continue;

        const newValueStr = `${beforeAt}@${sha}`;
        const newComment = `# ${candidate.versionText}`;

        // Only update if change is needed
        const currentShaPinned = isSha(ref) ? ref : null;
        const needsChange = currentShaPinned !== sha || !commentContainsVersion(parsed.comment, candidate.versionText);
        if (needsChange) {
          const newLine = rebuildUsesLine(parsed, newValueStr, newComment, quote);
          lines[i] = newLine;
          updated = true;
          totalUpdates++;
          console.log(`${file}:${i + 1} -> ${baseRepo}${subPath || ''} @ ${sha} (${candidate.versionText})`);
        }
      } catch (err) {
        console.warn(`Warning: Failed to update ${baseRepo} in ${file}: ${err.message}`);
      }
    }

    if (updated) {
      const content = lines.join('\n');
      if (content !== original) {
        await fsp.writeFile(file, content, 'utf8');
      }
    }
  }

  console.log(`Done. ${totalUpdates} update(s) applied.`);
}

function commentContainsVersion(comment, versionText) {
  if (!comment || !versionText) return false;
  return comment.includes(versionText);
}

function isSha(s) {
  return /^[0-9a-f]{40}$/i.test(s);
}

function rebuildUsesLine(parsed, newValueStr, newComment, quote) {
  const { indent, beforeKey, keyAndSep, trailing } = parsed;
  const quotedValue = quote ? `${quote}${newValueStr}${quote}` : newValueStr;
  // Always replace any existing comment with our version comment
  return `${indent}${beforeKey}${keyAndSep}${quotedValue} ${newComment}${trailing ? '' : ''}`;
}

function parseUsesValue(valueStr) {
  // valueStr like: owner/repo[/path]@ref
  const atIndex = valueStr.lastIndexOf('@');
  if (atIndex <= 0) return {};
  const beforeAt = valueStr.slice(0, atIndex);
  const ref = valueStr.slice(atIndex + 1);

  const parts = beforeAt.split('/');
  if (parts.length < 2) return {};

  const baseRepo = `${parts[0]}/${parts[1]}`;
  const subPath = parts.length > 2 ? `/${parts.slice(2).join('/')}` : '';
  return { beforeAt, ref, baseRepo, subPath, quote: detectQuoteChar(valueStr) };
}

function detectQuoteChar(s) {
  if (!s) return null;
  const first = s[0];
  if (first === '"' || first === "'") return first;
  return null;
}

function extractVersionFromCommentOrRef(comment, ref) {
  // Prefer version from inline comment like '# v2.3.4'
  const fromComment = extractVersionInfo(comment);
  if (fromComment) return fromComment;

  // Then try the ref itself if it is a version tag like 'v2.3.4' or 'v3'
  const fromRef = extractVersionInfo(ref);
  if (fromRef) return fromRef;

  return null; // Unknown currently installed version
}

function extractVersionInfo(str) {
  if (!str) return null;
  // Find first occurrence of v<major>[.<minor>][.<patch>], ignoring letters/digits before v
  const re = /(?:^|[^A-Za-z0-9])v(\d+)(?:\.(\d+))?(?:\.(\d+))?/i;
  const m = re.exec(str);
  if (!m) return null;
  const major = parseInt(m[1], 10);
  const minor = m[2] != null ? parseInt(m[2], 10) : 0;
  const patch = m[3] != null ? parseInt(m[3], 10) : 0;
  const versionText = `v${major}${m[2] != null ? `.${minor}` : ''}${m[3] != null ? `.${patch}` : ''}`;
  return { major, minor, patch, versionText };
}

function compareSemver(a, b) {
  if (a.major !== b.major) return a.major - b.major;
  if (a.minor !== b.minor) return a.minor - b.minor;
  if (a.patch !== b.patch) return a.patch - b.patch;
  return 0;
}

function pickBestRelease(releases, currentVersion) {
  // Filter out drafts/prereleases and releases without detectable version
  const candidates = releases
    .filter(r => !r.draft && !r.prerelease && r.versionInfo)
    .map(r => ({ tag: r.tag_name, versionInfo: r.versionInfo, versionText: r.versionInfo.versionText }));

  let filtered = candidates;
  if (currentVersion && Number.isFinite(currentVersion.major)) {
    filtered = candidates.filter(c => c.versionInfo.major >= currentVersion.major);
  }
  if (filtered.length === 0) {
    return null;
  }
  // Pick the highest by semver
  filtered.sort((a, b) => {
    const cmp = compareSemver(a.versionInfo, b.versionInfo);
    return cmp !== 0 ? cmp : 0;
  });
  return filtered[filtered.length - 1];
}

async function getReleasesForRepo(repo) {
  if (releasesCache.has(repo)) {
    return releasesCache.get(repo);
  }
  const [owner, name] = repo.split('/');
  const path = `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/releases?per_page=${RELEASES_PER_PAGE}`;
  const releases = await ghGetJson(path);
  if (!Array.isArray(releases)) {
    releasesCache.set(repo, []);
    return [];
  }
  // Attach extracted version info, try tag_name then name
  const withVersions = releases.map(r => {
    const vi = extractVersionInfo(r.tag_name) || extractVersionInfo(r.name);
    return { ...r, versionInfo: vi };
  });
  releasesCache.set(repo, withVersions);
  return withVersions;
}

async function getCommitShaForTag(repo, tag) {
  const key = `${repo}@${tag}`;
  if (commitShaCache.has(key)) return commitShaCache.get(key);

  const [owner, name] = repo.split('/');
  // Use the commits endpoint which resolves tag -> commit
  const path = `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/commits/${encodeURIComponent(tag)}`;
  const commit = await ghGetJson(path);
  const sha = commit && typeof commit.sha === 'string' ? commit.sha : null;
  if (sha) commitShaCache.set(key, sha);
  return sha;
}

function ghGetJson(apiPath) {
  const options = {
    hostname: 'api.github.com',
    path: apiPath,
    method: 'GET',
    headers: {
      'Accept': 'application/vnd.github+json',
      'User-Agent': 'gh-actions-updater-script',
    },
  };
  if (TOKEN) {
    options.headers.Authorization = `Bearer ${TOKEN}`;
  }

  return new Promise((resolve, reject) => {
    const req = https.request(options, (res) => {
      let data = '';
      res.setEncoding('utf8');
      res.on('data', chunk => { data += chunk });
      res.on('end', () => {
        if (res.statusCode && res.statusCode >= 200 && res.statusCode < 300) {
          try {
            resolve(JSON.parse(data));
          } catch (e) {
            reject(new Error(`Failed to parse JSON from ${apiPath}: ${e.message}`));
          }
        } else {
          // Return empty array for 404 on releases to avoid throwing on repos without releases
          if (res.statusCode === 404 && apiPath.includes('/releases')) {
            resolve([]);
            return;
          }
          reject(new Error(`GitHub API ${apiPath} failed: ${res.statusCode} ${res.statusMessage} - ${data.slice(0, 200)}`));
        }
      });
    });
    req.on('error', reject);
    req.end();
  });
}

async function findWorkflowFiles(dir) {
  let entries;
  try {
    entries = await fsp.readdir(dir, { withFileTypes: true });
  } catch (e) {
    return [];
  }
  const files = [];
  for (const ent of entries) {
    if (ent.isFile()) {
      if (/\.ya?ml$/i.test(ent.name)) {
        files.push(path.join(dir, ent.name));
      }
    }
  }
  return files;
}

function parseUsesLine(line) {
  // Capture indentation and the entirety after 'uses:'
  // Supports lines like:
  //   uses: owner/repo@ref # comment
  //   uses: "owner/repo@ref" # comment
  //   uses: 'owner/repo@ref' # comment
  const m = /^(\s*)(-?\s*)?(uses:\s*)(.*)$/.exec(line);
  if (!m) return null;

  const indent = m[1] || '';
  const beforeKey = m[2] || '';
  const keyAndSep = m[3];
  const rest = m[4] || '';

  // Parse value and comment from rest while respecting optional quotes
  let i = 0;
  while (i < rest.length && /\s/.test(rest[i])) i++;

  if (i >= rest.length) {
    return {
      indent,
      beforeKey,
      keyAndSep,
      valueStr: '',
      comment: '',
      trailing: '',
      originalLine: line,
    };
  }

  let quote = null;
  let value = '';
  let j = i;
  if (rest[j] === '"' || rest[j] === "'") {
    quote = rest[j];
    j++;
    const start = j;
    while (j < rest.length) {
      if (rest[j] === quote && rest[j - 1] !== '\\') break;
      j++;
    }
    value = rest.slice(start, j);
    // Move past closing quote if present
    if (j < rest.length && rest[j] === quote) j++;
  } else {
    const start = j;
    while (j < rest.length && !/\s/.test(rest[j]) && rest[j] !== '#') {
      j++;
    }
    value = rest.slice(start, j);
  }

  // Skip spaces
  while (j < rest.length && /\s/.test(rest[j])) j++;

  let comment = '';
  if (j < rest.length && rest[j] === '#') {
    comment = rest.slice(j).replace(/^\s*#\s?/, '').trim();
  }

  return {
    indent,
    beforeKey,
    keyAndSep,
    valueStr: value,
    comment,
    quote,
    trailing: '',
    originalLine: line,
  };
}

(async () => {
  try {
    await main();
  } catch (e) {
    console.error('Error:', e);
    process.exit(1);
  }
})();