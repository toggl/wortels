wortels
=======

An asset packager, similar to Rails asset pipeline. Assets are listed in a **manifest file**, like this:

```
underscore.js
backbone.js
app.js
```

Wortels reads the asset manifest file, then for each asset computes a SHA1 of the file contents. 
If the asset wasn't previously packaged, it's compiled/transpiled/whatever you need, and the resulting
file is stored in a **cache folder**, that is shared per user. If you package an asset file
in one project, the packaged file can be later used in other projects as well, when using wortels.

If an asset file was already packaged, the previously packaged result is fetched from the cache folder
and there's no need to package it again. This makes the whole packaging process very fast, since
typically only some files mentioned in the asset manifest have actually changed (and therefore their
SHA1 hashes have changed, which triggers re-packaging of the changed files).

Currently only Closure compiler is supported, but the code can easily be modified to support other
compilers as well.
