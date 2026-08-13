/* Link the page's own Markdown from the footer.
 *
 * The header dropdown already offers the same thing, but it is a dropdown: you
 * have to know it is there and that one of its five entries is the one you
 * want. Somebody who has read to the bottom of a page and now wants to hand it
 * to an assistant should find it where they already are.
 *
 * The address is not built from location.pathname. That would mean guessing the
 * source path — docs/<lang>/<page>.md — from the published one, and the guess
 * has to know that the default language has no folder while every other one
 * does, that a section index is index.md rather than <section>.md, and that the
 * file name stays English in both trees. MkDocs already knows all of it and
 * writes the answer into the "edit this page" link at the top of the page, so
 * that link is the source and /edit/ becomes /raw/. Two consequences worth
 * knowing: theme.features must keep content.action.edit, and a page with no
 * edit link simply gets no footer link rather than a wrong one.
 */
document.addEventListener("DOMContentLoaded", function () {
  var LABEL = {
    de: "Diese Seite als Markdown",
    en: "This page as Markdown",
  };
  var TITLE = {
    de: "Die Quelle dieser Seite als reines Markdown — zum Weitergeben an ein Sprachmodell",
    en: "The source of this page as plain Markdown — to hand to a language model",
  };

  var edit = document.querySelector('a.md-content__button[href*="/edit/"]');
  var copyright = document.querySelector(".md-copyright");
  if (!edit || !copyright || copyright.querySelector(".llm-source")) {
    return;
  }

  // Only the first occurrence, and only the path segment: a repository whose
  // name contained "edit" would otherwise be rewritten too.
  var raw = edit.getAttribute("href").replace("/edit/", "/raw/");

  var lang = document.documentElement.lang === "de" ? "de" : "en";
  var line = document.createElement("div");
  line.className = "llm-source";

  var link = document.createElement("a");
  link.href = raw;
  link.target = "_blank";
  link.rel = "noopener";
  link.title = TITLE[lang];
  link.textContent = LABEL[lang];

  line.appendChild(link);
  copyright.appendChild(line);
});
