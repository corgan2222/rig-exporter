/* Move the "Copy page" control into the header bar.
 *
 * The plugin mounts it next to the page's h1 (`.md-content h1`), which puts it
 * inside the banner on the start page and shifts the first heading everywhere
 * else. It belongs with the other page-level controls — search, colour scheme,
 * repository — so it moves next to them.
 *
 * The plugin builds its markup at runtime, so the node may not exist yet when
 * this runs. Try once, and if it is not there, watch until it appears and stop
 * watching the moment it does. The timeout is the give-up: if the plugin ever
 * stops emitting that class, this leaves no observer running forever.
 */
document.addEventListener("DOMContentLoaded", function () {
  function relocate() {
    var control = document.querySelector(".copy-to-llm-split-container");
    var header = document.querySelector(".md-header__inner");
    if (!control || !header || control.dataset.movedToHeader) {
      return false;
    }
    // Before the repository link, so the two least-used controls sit together
    // and the search field keeps the middle of the bar.
    header.insertBefore(control, header.querySelector(".md-header__source"));
    control.dataset.movedToHeader = "1";
    return true;
  }

  if (relocate()) {
    return;
  }

  var observer = new MutationObserver(function () {
    if (relocate()) {
      observer.disconnect();
    }
  });
  observer.observe(document.body, { childList: true, subtree: true });
  setTimeout(function () {
    observer.disconnect();
  }, 5000);
});
