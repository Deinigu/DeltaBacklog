# DeltaBacklog: user manual

DeltaBacklog is your videogame library on your own Raspberry Pi. You can look at your games, filter them in any way you like, keep lists such as *Playing now* and *Up next*, record the years you played each game, and add new games with their cover and details filled in for you. It is made to be used on a phone first, and it works just as well on a PC.

This manual is task by task. For how the program is built, see the [developer manual](developer-manual.md).

**Contents**
1. [Opening the app and installing it on your phone](#1-opening-the-app-and-installing-it-on-your-phone)
2. [Finding your way around](#2-finding-your-way-around)
3. [The Library: cards, search and filters](#3-the-library-cards-search-and-filters)
4. [Sorting and grouping](#4-sorting-and-grouping)
5. [Saved views](#5-saved-views)
6. [Changing a game](#6-changing-a-game)
7. [Putting games in your own order](#7-putting-games-in-your-own-order)
8. [The table (spreadsheet) layout](#8-the-table-spreadsheet-layout)
9. [Adding a game](#9-adding-a-game)
10. [Covers](#10-covers)
11. [What the little icons mean](#11-what-the-little-icons-mean)
12. [Series progress](#12-series-progress)
13. [Year in review and rankings](#13-year-in-review-and-rankings)
14. [Stats](#14-stats)
15. [Surprise me: let the app choose](#15-surprise-me-let-the-app-choose)
16. [Link your games with IGDB](#16-link-your-games-with-igdb)
17. [If something looks wrong](#17-if-something-looks-wrong)
18. [Backups and what is still missing](#18-backups-and-what-is-still-missing)
19. [Screenshots](#19-screenshots)

---

## 1. Opening the app and installing it on your phone

Open **http://deltabacklog.local** in any browser (no port number needed). Use the same address on your phone and on your PC, as long as they are on your home network.

If that name does not open (some Android phones cannot find `.local` names), use the Raspberry Pi's number instead, for example `http://<address-of-your-pi>:8081`. Both lead to the same app.

To open it with one tap from your phone's home screen, add a shortcut:
- **Android (Chrome):** open the menu (⋮) and choose **Add to Home screen**.
- **iPhone (Safari):** tap the Share button and choose **Add to Home Screen**.

Because the Pi is reached over plain `http://`, browsers treat this as a shortcut that opens the page, not as an installed app, and the app does not keep an offline copy: it needs the Pi to be reachable.

The app follows your phone's light or dark setting. On a PC, the **Theme** button at the bottom of the left sidebar switches between them.

## 2. Finding your way around

![The Library on a phone](img/library-phone.jpg)

**On a phone** the bar at the bottom has **Library**, your first two pinned views (for example *Playing now* and *Up next*) and **More**. **More** opens a list with **Series** ([section 12](#12-series-progress)), **Stats** ([14](#14-stats)), **Year in review** ([13](#13-year-in-review-and-rankings)), **Rankings** ([13](#rankings)), **Screenshots** ([19](#the-screenshots-page)) and **Views** (where you manage your views), plus **Link IGDB** ([16](#16-link-your-games-with-igdb)) while there is something to review. It is highlighted while you are on one of those pages. The round **+** button adds a game.

**On a PC** the same things are in the left sidebar, with every pinned view and every page listed directly (there is no *More*). Two keys help: press **/** to jump to the search box and **n** to add a game.

![The Library on a PC](img/library-desktop.jpg)

The **More** list on a phone:

![The More list](img/more-phone.jpg)

## 3. The Library: cards, search and filters

The Library shows every game as a card.

**What is on a card**
- The cover, with the game's **status** in the bottom-left corner (and a coloured line under the cover in the same colour).
- A **★** in the top-right corner for favourites.
- A small emoji in the top-left corner when the game is *not* a plain "Owned" game (for example 🏴‍☠️ or 🆓, see [section 11](#11-what-the-little-icons-mean)).
- Under the title: the **platforms** (the first two) and the **years you played it**. The newest years are shown as small pills. A dashed **+3** means three older years are not shown. 🧒 means you played it as a child and ♾️ means you have always played it. Touch or hover over the years to read them in full.

**Search.** Type in the search box. It looks in the title, series, developer and notes, and it ignores accents, so "pokemon" finds *Pokémon*.

**Filters.**

![The filter sheet](img/filters-phone.jpg)

Tap the filter button (the three lines next to the search box). On a PC the filters are always shown on the left.

- **Tap a chip once to include it.** For example *Platform: PC* shows only PC games.
- **Tap it again to exclude it.** The chip turns red and is crossed out. For example *Finished* excluded hides finished games.
- **Tap a third time to clear it.**
- When you include two or more values in *Platform*, *Genre*, *Theme*, *Game mode*, *Perspective*, *Year played* or *Played with*, a switch **Any / All** appears. *Any* shows games that have at least one of them. *All* shows only games that have every one of them (for example games that are both *RPG* and *Indie*).
- At the top are quick switches: **Favorite**, **VR**, **100% completed** and, once IGDB times exist, **Short game (under 10 h)**. Games IGDB has no time for are never shown as short.
- **Game mode** (single player, co-op, multiplayer...) and **Perspective** (first person, third person, isometric...) come from IGDB and appear in the filters as soon as some game has them.
- The number on each chip is how many games would match if you tapped it, taking your other filters into account. Chips that would give nothing are faded.
- The active filters appear as pills under the search box. Tap a pill's **✕** to remove it, or **Reset** to clear everything.
- The button at the bottom shows how many games match (*Show 27 games*).

The address in your browser changes with your filters, so you can bookmark a filtered list.

## 4. Sorting and grouping

![The sort sheet](img/sort-phone.jpg)

Tap the sort button (the up/down arrows at the right of the search box).

- **Sort levels.** Games are sorted by the first level. When two games tie, the next level decides, and so on. Each level has a field, an arrow for ascending or descending (↑ / ↓), buttons to move it up or down, and ✕ to remove it. **Add level** adds another one.
- **Fields you can sort by:** Title, Status, Platform, Series, Release year, Last year played, First year played, Years played (count), Date added, Date edited, Favorites first, 100% completed first, and Manual order.
- Titles sort naturally ("Fallout 2" comes before "Fallout 10"), and games that have no value for a field always go last.
- **Default order** is: Status, then your Manual order, then Title.
- **Group by** splits the list into sections: Status, Platform, Series, Genre, Year played or Ownership. A game that was played in several years appears under each year, and one with several genres under each genre.
- **Reset** returns to the default order.

## 5. Saved views

A **view** is a name for a set of filters, a sort order and a layout, so you can open it with one tap.

**Starter views.** You start with five: **Playing now**, **Up next**, **Backlog**, **On hold** and **Endless**. The first three are pinned; the bottom bar of a phone shows the first two (the sidebar shows all). They are ordinary views: change them or delete them.

**Make your own.**
1. Set up filters, sorting and (if you like) grouping or the table layout in the Library.
2. Tap **Save as view**, type a name and an emoji, and tap **Save**.
3. The view now appears in the sidebar and, if it is one of your first two pinned views, in the bottom bar of a phone. Other pinned views are reached through **More → Views**.

**Using a view.**
- Open it from the bar. If you then change its filters or sort, a **Save** button appears. Tap it to keep the changes, or **Revert** to go back to how it was saved.
- **Save as…** makes a new view from the current state without touching the old one.
- Tap the view's title to rename it.

![The Views page](img/views-phone.jpg)

**Manage views.** Open **Views** (**More** on a phone, or the sidebar). For each view you can open it, **📌 pin** it to the bar, move it **▲▼**, rename it **✎**, duplicate it **⧉**, or delete it **🗑**. After a delete there is an **Undo** for a few seconds.

## 6. Changing a game

Tap a card to open the game.

![A game's details](img/game-phone.jpg)

**Quick changes** (at the top, saved at once)
- **Status:** pick Playing, Playing - Evergreen, Up Next, Backlog, Hold, Evergreen, Finished, Dropped or Not Interested. A game that changes status goes to the end of your manual order.
- **★ Favorite** switches the favourite mark.
- **The year is added for you.** When you set a game to **Playing**, **Playing - Evergreen** or **Finished**, the current year is added to its years played (once).

**What IGDB adds** (after the one-off command in section 16): the game's **Game modes** and **Perspective** appear in the details, and buttons for the game's **Steam**, **GOG**, **Epic Games** or **official site** page open in a new tab. These cannot be edited here; they are IGDB's.

**Time to beat.** The details have a **Time to beat** block with three tiles: **Main story**, **Main + extras** and **Completionist**, each with a small bar showing how long it is compared with the longest. A note says where the numbers come from: *from IGDB* (filled in by the command in section 16), *from HowLongToBeat* or *set by you*. The times are not shown on the cards, only here (the **Short game** filter, Surprise me's **Quick game** and the backlog hours on Stats still use them).
- **✎ Edit times** opens three boxes. Type hours (`7.5`, `7,5`, `12`), hours and minutes (`7h30`, `1:30`) or minutes (`45m`); leave a box empty for no time. **Save** marks the times as *set by you*.
- **Get from HowLongToBeat** looks the game up by its title and release year and lists the matches with their three times. Tap the right one and its times are saved at once (you can still edit them afterwards). The search box lets you try another title when the game is not found. If HowLongToBeat cannot be reached (it has no official service, and it can be blocked or change without notice) the block says so and nothing else is affected: type the times yourself.
- Times you set or took from HowLongToBeat are **never replaced** by IGDB's later reads. Games with no time show "No time yet".

![The time to beat block](img/time-to-beat-phone.jpg)

**Edit** changes everything else.

![Editing a game](img/edit-phone.jpg)

Tap **✎ Edit**. You can change the title, platforms, genres, themes, series, developer, release year, ownership, the years played, who you played with, VR, 100% completed and notes. Tap a list (like *Genres*) to open a sheet where you tick values and can type a new one. For **Played in** the list offers the years from the earliest one in your library to this year; type any other year (4 digits, from 1970 to 2100) to add it, or the periods *Childhood* and *Always*.
- **Save** keeps your changes. **Cancel** or the ✕ asks before throwing away unsaved edits.
- **Fill from IGDB…** looks the game up on IGDB, lets you pick the right match, and copies its genres, themes, developer, series and release year into the form. Your platforms, notes and ownership are not touched. Check the form, then **Save**. The game is then linked to IGDB (shown as *Linked to IGDB*, with **Unlink** if it was the wrong match).

**Change cover** is explained in [section 10](#10-covers). **Delete game** removes the game for good, after a confirmation: its years, cover and screenshots go with it (the picture files are deleted too) and it cannot be undone.

## 7. Putting games in your own order

The *manual order* is how you rank games yourself, for example the order you plan to play your *Up next* games.

![Reordering a view](img/reorder-phone.jpg)

1. Open the view or the Library list you want to order (for example *Up next*).
2. Tap **⇅ Reorder** (on a phone, the icon with two arrows).
3. Drag the **⠿** handle to move a game, or use the **▲ ▼** buttons.
4. Tap **✓** (Done).

What to know:
- The order is saved as soon as you move a game.
- Only the games in the list are moved. Your other games keep their place.
- One order is shared by all views. A view shows it when it sorts by *Manual order*, which the default order does.

## 8. The table (spreadsheet) layout

Tap **☰ Table** (next to the title) to see your games as a table. Tap **▦ Cards** to go back. The layout is part of a saved view, so you can keep a table view of your backlog.

![The table on a PC](img/table-desktop.jpg)

- **Sort** by tapping a column heading. Tap again to reverse it.
- **Resize** a column by dragging the edge of its heading.
- **Columns** lets you choose which columns to show (title, status, platforms, genres, themes, years played, series, developer, released, ownership, played with, favorite, VR, 100%, notes, added). **Defaults** brings the original set back. Your choices are remembered on this device.
- **Edit in place:** tap a cell and type. **Enter** saves and moves down, **Tab** saves and moves right, **Esc** cancels. Status is a drop-down, checkboxes toggle with one tap, and lists (platforms, genres, years...) open a sheet. Notes can have several lines (**Shift+Enter** for a new line).
- The ↗ button at the start of a row opens the game.
- **Export CSV** saves exactly the rows and columns you see as a file you can open in Excel or Google Sheets.
- On a phone the table scrolls sideways, with the title column kept in place.

## 9. Adding a game

Tap **+**.

![Searching for a game to add](img/add-search-phone.jpg)

1. **Type the title.** After a moment you see matching games from IGDB, with cover, year, developer and genres. A match that is already in your library has an orange **Already in your library** label.
2. **Tap the right one.**
3. **Check the form.** Genres, themes, developer, series and release year are filled in for you. You choose the **status** (Backlog by default), the **platforms**, the **years played** (*Played in*), **played with** and the **ownership** (Owned by default). Tap **More details** for genres, themes, series, developer, release year, favorite, VR, 100% completed and notes: everything the game's Edit form has.
   - **Years played:** the list runs from the earliest year already in your library to this year, plus *Childhood* and *Always*. For an older year, type its 4 digits in the box at the top of the list and tap *Add*. A game added as Playing, Playing - Evergreen or Finished also gets the current year, once.
   - **Platforms:** the platform you play most among the ones the game is on is selected for you (for a PC game that is PC). The other platforms IGDB lists appear as chips: tap one to add or remove it.
   - **Cover:** a row of pictures from SteamGridDB appears. The first is chosen. Tap another one, or **No cover**. **Search again** looks again with the title you typed.
4. **Add to library.** The game is added at the end of your manual order and opens so you can adjust it.

If the game is not found, tap **Add "..." without a lookup** to type everything in yourself. The lookups need keys that are set up once on the Pi (see the [developer manual](developer-manual.md#3-running-it)). Without them you can still add games by hand.

## 10. Covers

Open a game and tap **Change cover**.

![Choosing a cover](img/cover-picker-phone.jpg)

- Two tabs: **SteamGridDB** (community covers; wide pictures match your library) and **IGDB** (official artwork, screenshots and the box cover, each labelled). The tab you used last is remembered.
- The search box starts with the game's title. Type another title and tap **Search** when the automatic match is the wrong game.
- Tap a picture to use it. It is saved on your Raspberry Pi, so the app does not depend on those websites afterwards.
- Cards show the cover as a wide picture, so wide images look best.

### The poster (portrait picture)

The rankings and their PNG use a **portrait poster** of each game, made for you automatically. To choose your own, open a game and tap **Change poster**, or, on a ranking board, tap a game and then the picture button in the bar at the bottom.

- **IGDB** shows the official box cover and **SteamGridDB** shows several community portraits. Tap one to use it. Both lists load on their own: if SteamGridDB cannot be reached right now (some providers block it at times) its part says so and the IGDB cover is still there.
- **Paste an image link** uses a picture from the web. Only links from the same sites as covers (IGDB and SteamGridDB images) are accepted.
- **Crop of the wide cover** cuts the middle of the game's wide cover into a portrait (no internet needed). **Fetch automatically again** goes back to what the app chooses by itself (IGDB, then SteamGridDB, then the crop) and replaces your pick.
- A poster you chose stays: the app never replaces it on its own, and changing the wide cover does not change it. The ranking board and the exported PNG show the new poster at once.

## 11. What the little icons mean

**Ownership** (filters, the table, the game's details and a badge on cards):

| Icon | Meaning |
|---|---|
| ✅ | Owned (shown on cards only for the other kinds, to keep them calm) |
| 🏴‍☠️ | Pirated |
| 🆓 | Free to play |
| 👨‍👩‍👧 | Family share |
| 🤝 | Borrowed |
| 🚫 | Nope |
| 🏷️ | Any other value you add yourself |

**Years played on a card:** 🧒 childhood · ♾️ always played · a dashed **+N** older years not shown.

## 12. Series progress

Open **Series** in the navigation (**More** on a phone) to see how far you are through every series. Each series is a tile: a fan of its posters, its name, a progress bar and *2/4 finished*. Tap a tile to open the series on its own page (the phone's back button returns to the tiles): its games are posters in release order, with a line such as *3 finished · 4 played · 5 games · 1 left to play*. Tap a poster to open the game's details. A green ✓ on a poster means finished, a ▶ means played; posters of games you have not played yet are a little paler.

- **Finished** means the game's status is **Finished**.
- **Played** means you have started it: it is Finished, or its status is Playing, Playing - Evergreen, Evergreen, Hold or Dropped, or it has any year (or Childhood / Always) in *Played in*. Finished games count as played too.
- **Left to play** means a game you have not played yet (for example Backlog or Up Next). Games marked **Not Interested** are not counted as left to play, but they still count in the total.
- The bar is green for finished games, in the accent colour for games played but not finished, and grey for the rest.
- **Games left to play** (the button at the top) hides series where nothing is left. **Not in your library** shows only the series that have a game you do not have yet (see below). Both stay on when you open a series and come back.
- Games are listed by **release year** (games without a year come last), then by title. There is no "number in the series": the release year puts a game in its place. Games imported from another tool usually have no year yet: open a game, tap **✎ Edit** and **Fill from IGDB…**, and the year is set. The page tells you how many games still have no year.
- A game's details show where it falls, for example **3 of 5 in Ace Attorney**, only when every game of that series has a release year (otherwise they show *Ace Attorney (5 games)*, because the order would be a guess). It appears only for series with at least two games. A series is every game with the same series name; capital letters do not matter.

![The Series page](img/series-phone.jpg)

### Games you do not have yet ("Not in your library")

An opened series also shows the games of the same IGDB collection that are not in your library, in release order among yours. They appear greyed, with a dashed outline, the label **Not in your library**, and two buttons:

- **Add** opens the add form already filled in from IGDB (genres, developer, release year, cover, and the series name you use). You choose the status, platforms and the rest, then **Add to library**. The game then disappears from the not-in-your-library list by itself.
- **Hide** removes the game from this list for good (for example a spin-off you will never play). A message offers **Undo**; hidden games are listed under **Hidden (n)** at the bottom of the series, each with **Show again**.

Only real games are listed: DLC, expansions, bundles, ports, updates, mods, episodes and seasons are left out; remakes and remasters are kept. Games are matched by their IGDB link, so a game you have not linked to IGDB can show up here too (use **Link IGDB** to link your games first).

Which IGDB collection belongs to a series is decided by your linked games: the series *Souls* with Dark Souls linked gets Dark Souls' collection, whatever IGDB calls it. A series with no game linked to IGDB shows nothing. The lists are saved on the server and are **not** read when you open the page. Tap **↻ Refresh from IGDB** to read them (it takes about half a minute for a big library and keeps going if you leave the page); the page says when they were last read. **`deltabacklog sync-igdb --enrich`** refreshes them too. Big series (Pokémon, Mario) can list dozens of games: hide the ones you do not care about.

![A series opened, with games that are not in the library](img/series-shelf-phone.jpg)

## 13. Year in review and rankings

**Year in review** (under **More** on a phone) opens the latest year you played something. A game belongs to a year when that year is in its *Years played* (moving a game to Playing or Finished adds the current year by itself). Childhood and "always played" do not count as a year.

- **The page.** The big **‹** and **›** buttons go to the previous and next year (one calendar year at a time, so a year with no games is reachable too; it says nothing was recorded). The strip under the title jumps to any year, with the number of games.
- **Counts.** Games are grouped by their *current* status: **Finished**, **Dropped**, **Still playing** (Playing and Playing - Evergreen) and **Played** (everything else, for example Evergreen or Backlog). Next to them are the platforms and genres you played that year. Tap any game to open its details.
- **Rank this year** opens the ranking of that year, or makes it with that year's games waiting to be ranked (see Rankings below).

![A year in review](img/year-phone.jpg)

### Rankings

**Rankings** (under **More** on a phone, in the sidebar on a PC) are lists like the ones you make on TierMaker: as many as you want, each with its **own tiers**, **your own labels** ("Me ha cambiado la vida", "Vaya mierda"...) and colours, and big **poster** pictures of the games. A ranking can hold any set of games: everything you played in 2025, all your RPGs, a hand-picked handful.

![The rankings list](img/rankings-phone.jpg)

- **The list.** **New ranking** asks for a name and the starting tiers: **S to F** or **Empty** (you make the tiers). Each ranking shows its best games, how many games it has and its tier colours. Under it: move it up or down, rename, duplicate and delete (the games stay in your library). The old per-year tier lists became rankings called *Tier list 2025*, *Tier list 2024*... with the same tiers and games.
- **Add games.** The button opens your whole library with search and the same filters as the Library page. Tick games one by one, or filter (for example *Year played: 2025*) and tap **Select these** to take every match at once, then **Add**. New games wait in **Not ranked**, below the board. The first time a game joins a ranking its poster is fetched in the background (a few seconds each; the page fills them in by itself). Until then the wide cover is shown.
- **Move a game.** On a PC drag it with the mouse. On a phone **press and hold** until it lifts, then drag (near the top or bottom edge the page scrolls by itself). Or **tap a game** to select it: the bar at the bottom has **◀ ▶** (earlier or later in its tier), **▲ ▼** (one tier up or down), *Not ranked*, *Details* and *Remove from this ranking*, and with a game selected you can also **tap a tier** to put it there. With the keyboard: Tab to a game, **Enter** selects it, the **arrow keys** move it (left and right inside the tier, up and down between tiers), **Esc** lets go. Without a selection the arrows just move between games.
- **Tiers.** **Tap a tier's name** (with nothing selected) to change its **label** (up to 80 characters, it wraps), its **colour** (a palette or any custom colour), move it up or down, **duplicate** it or **delete** it (its games go back to *Not ranked*; **Undo** in the message at the bottom brings it back). **Add tier** adds a new one at the end; the menu **⋯** can add the classic S to F tiers.
- **Change a poster.** Tap a game and then the picture button in the bar at the bottom: see [The poster](#the-poster-portrait-picture).
- **Look.** **S / M / L** change the size of the posters and **Titles** writes the game name on each poster (on a PC hover a game to see its name; on a phone tap it and the bar at the bottom shows it). Both are remembered on this device. The title and the **⋯** menu (rename, description, duplicate, delete) are at the top.
- **Export PNG** saves the board as a picture (dark background, tier labels, full-size posters; the *Not ranked* games are left out), named after the ranking.
- Everything is saved automatically. Opening a year's ranking from **Rank this year** also adds the games that got that year since (a game you removed from it comes back too); renaming the ranking does not break that link.

![A ranking](img/ranking-phone.jpg)

On a PC the same board has room for many posters per row:

![A ranking on a PC](img/ranking-desktop.jpg)

If you delete a game from the library it leaves every ranking.

## 14. Stats

Open **Stats** in the navigation (**More** on a phone). Everything is worked out from the games you already have; nothing is stored.

- **Backlog**: how many games are on the backlog or up next, **at least how many hours** that is (the "Main + extras" time of each game, or the closest time known when it has no such time, added up, whatever its source; games with no time to beat yet are left out, so it is a minimum, and the card says how many games it covers: when fewer than half have a time it leads with that instead), and how many per platform (a game on two platforms counts for both). Before the IGDB details have been read it says there is no time to beat yet.
- **Finished per year**: a bar chart of games with the status Finished, counted in the last year you played them. Finished games you played in *Childhood* or *Always* (no calendar year) and finished games with nothing recorded are mentioned separately under the chart, not drawn.
- **Time waiting**: for Backlog, Up Next and Hold, the typical and longest time since a game was last changed, and the five games that have waited longest (tap one to find it in the Library). This is a rough guide: any edit resets the clock. Games you have not touched since the import are left out (the card says how many), and until the rest were changed over at least a week it tells you there is nothing to compare yet.
- **Most played genres**: genres of the games you have played (a year or period recorded, or a status such as Playing, Finished or Dropped).
- **Finished or dropped**: the share of games you finished against dropped.

![The Stats page](img/stats-phone.jpg)

The charts have text labels and work in light and dark. Screen readers get the numbers as a table.

## 15. Surprise me: let the app choose

Can't decide what to play? Tap **🎲 Surprise me** at the top of the Library or of any saved view (on a phone it is the dice icon, from 600px wide it also has its name). On a phone, **Save** and **Save as…** sit in the row under the search box (next to **Revert**), so the top row and the view name stay as they are.

**What it picks from.** The games that Library or view is showing right now, including your search and filters. With no filters at all it picks from your **Up next** games, or from **Backlog** if Up next is empty. Games that are Finished, Dropped, Not Interested or Evergreen are never offered, unless you filtered to that status on purpose.

**Your mood (optional).** A sheet opens first. Tap any of these to narrow the pick; the number on each chip is how many games would be left:
- **Platform** and **Genre**: tap one or more (a game needs to match any of the ones you tap in that row).
- **VR only**.
- **Quick game**: games that take under 10 hours to beat, according to IGDB. It only appears once IGDB times exist; games without a time are not offered by it.
- **Not played recently**: games you never played, or whose last year played is two or more years ago. A game you play "always" never counts.

If nothing fits, **Pick one** is greyed out and says so; remove a choice. Then tap **Pick one**.

**The result.** You see the cover, the title, platforms, genres and the years you played it (the time to beat is only in the game details), and three buttons:
- **▶ Play it** sets the game to *Playing* (the app records the current year for you) and closes the sheet. An Evergreen game becomes *Playing - Evergreen* instead, and a game you are already playing is left as it is (the button then reads *Already playing*).
- **Another one** shows a different game. It never shows the same game twice until you have seen them all; then it tells you and offers **Start over**.
- **Skip** is like *Another one*, and also leaves that game out for the rest of your browser session (until you close the tab). If you skip everything, **Forget my skips** brings back the skipped games that fit this pick (skips from other views stay skipped).

![Choosing a mood](img/surprise-mood-phone.jpg)

![The result](img/surprise-result-phone.jpg)

The **←** button goes back to the mood, and **✕** (or Escape) closes the sheet without changing anything.

## 16. Link your games with IGDB

Games imported from your old table are not yet connected to their page on IGDB, the game database the app uses for covers and details. A one-off command on the Pi, `deltabacklog sync-igdb`, connects them (the steps are in the [developer manual](developer-manual.md#8-external-services); `--dry-run` shows what it would do without changing anything). Restart the app afterwards so it shows the new links. It is safe: it first saves a copy of your library, and it only sets the link. It never changes anything you typed, and the app does not show IGDB's scores.

Most games are linked automatically. The ones the app is not sure about wait for you in **Link IGDB** (an entry in the navigation that only appears while there is something to review).

For each game you see its title, year and platforms, and IGDB's best guesses with the platforms IGDB lists for each (handy when two games share a name) and a percentage that says how closely the title matches. A low percentage means the app found no close match and shows IGDB's top results anyway, in case one is right. **IGDB ↗** opens that game's page on igdb.com:
- Tap **Link** next to the right guess.
- **Search IGDB…** lets you look for it yourself; tap the right result.
- **Not on IGDB** if the game is not there at all (a fan game, a regional title). The app will not ask again.
- **Skip** leaves it for later; it comes back the next time you open the screen, or with **Review them again**.

![The Link IGDB screen](img/link-phone.jpg)

If IGDB's game is already linked to another game in your library you are told which one, and nothing changes. A game that already has a link (for example linked on another device) is never re-linked from this screen.

**Second step: the details.** After the games are linked, a second command reads each linked game from IGDB (`deltabacklog sync-igdb --enrich`, see the developer manual). Run its `--dry-run` first: it only prints what it would do. It saves a copy of your library first, too. It **only fills empty fields** (release year, genres, themes, developer, and the platform only when IGDB lists exactly one and your library already has that platform; it never adds a platform). A **series** is only filled when IGDB's collection matches a series you already have, or when two or more of your games share it, because IGDB gives almost every single game a collection named after itself; the command's report lists the ones it left out, so nothing you typed is ever changed, and it adds what the app did not have: time to beat, game modes, perspectives and store links. Still no scores. If you later link a game to another IGDB game, or unlink it, its IGDB time, modes, perspective and store buttons are removed; run the command again to read the new ones. A series you remove from a game stays removed. Running the command again with `--refresh` only updates the IGDB data (times, modes, perspectives, store buttons) and leaves everything else as it is. At the end it also reads the IGDB collections of your series, which feed the *Not in your library* games on the Series page ([section 12](#12-series-progress)). Restart the app afterwards to see the new data.

## 17. If something looks wrong

- **The page looks old after an update.** Reload it twice (on a PC: Ctrl+Shift+R). If it still looks old, clear the site's data in your browser settings once.
- **"Lookups are not set up".** The IGDB or SteamGridDB key is missing on the Pi. Games can still be added by hand.
- **A page saying "unknown host".** The app only answers to its own address, `deltabacklog.local`, the Pi's hostname and IP addresses. If you open it by another name (a router name, a Tailscale name, a reverse proxy), add that name to `ALLOWED_HOSTS` in `/etc/deltabacklog.env` on the Pi (for example `ALLOWED_HOSTS=raspberry.lan`) and restart the app with `sudo systemctl restart deltabacklog`.
- **A cover is missing.** Open the game, tap **Change cover** and pick one. Adding a game never fails because of a cover: if the picture could not be downloaded you are told, and the game is added anyway.
- **A change did not save.** You see a message at the bottom. The game goes back to how it was, so try again.
- **I added the same game twice.** The add form warns you when a game with the same title or IGDB link is already there. Open the extra copy and use **Delete game**.

## 18. Backups and what is still missing

**Backups happen by themselves.** Every night at 03:30 the Raspberry Pi saves a copy of your library and keeps the last 14 days. Restoring one is described in the [developer manual](developer-manual.md#11-build-and-deploy).

**Screenshots are backed up too.** Each night the new pictures are copied to a second place, so the originals and the copy are on different disks (if the pictures live on the external drive, the copy goes to the SD card, as long as the SD card keeps 5 GB free). Pictures you delete in the app stay in that copy until you clear it by hand. When the pictures are on the external drive, each night's library copy is also saved on that drive. Details and restore steps are in the developer manual.

**Not built yet:** a full export of everything (the **Export CSV** button of the table, [section 8](#8-the-table-spreadsheet-layout), already saves the games you see).


## 19. Screenshots

Every game can keep its own pictures, for example your best moments or where you stopped.

![Screenshots in a game's details](img/screenshots-phone.jpg)

**Add pictures.** Open a game and scroll to **Screenshots**. Tap **＋ Add screenshots** and pick one or several pictures (on a phone, from the photo library or the files; on a computer you can also **drag image files onto the section**). JPEG, PNG and WebP are accepted, up to 25 MB each. You see the progress of every file; if one fails (wrong type, too big, too many pixels) the reason stays next to its name and the others still go in. The original file is kept exactly as it was, with full quality; a small copy is made for the grid.

**Look at them.** Tap a picture to open it full screen. Swipe or use the arrow keys (or the ‹ › buttons) to move between pictures, and pinch to zoom. The date shown is when the picture was taken, when the file knows it.

![The full-screen viewer](img/screenshot-viewer-phone.jpg)

**In the viewer**
- **Caption:** type under the picture; it saves when you leave the box.
- **The download icon** (arrow pointing down) saves the original file.
- **◀ Move earlier / Move later ▶** changes the order of the pictures.
- **Delete** removes the picture. For a few seconds a message offers **Undo**.

**Where the files live.** On the Raspberry Pi, in the screenshots folder (on the external drive when it is set up that way); the app only stores the picture list in its database. If the drive is not connected, you see "Screenshot storage is not available", adding pictures does not work, and the rest of the app keeps working; connect the drive and restart the service. Deleting a game deletes its pictures.

### The Screenshots page

**Screenshots** (under **More** on a phone, in the sidebar on a PC) shows every picture of every game in one place, so you can browse them without opening each game.

![The Screenshots page on a phone](img/gallery-phone.jpg)

- **By game** (the default) puts the pictures under a header with the game's cover and name; tap the header to open the game. **All, newest first** drops the headers and lists everything by date (when the picture was taken, or when you added it if the file does not know). The choice is remembered on this device.
- **Search** finds a game by name or a picture by its caption. **Year** keeps one year only.
- Long lists load 120 pictures at a time: tap **Show more** at the bottom (the viewer also loads the next ones by itself when you swipe to the end).
- Tap a picture to open the full-screen viewer. Swipe or use the arrow keys to go through **all the pictures that match** (not only that game's). It shows the game's name, the caption, the date and the size, **Original** opens the file in a new tab, the download icon saves it, and **Open game** goes to the game. Captions, order and deleting are done from the game's own page.
- With no pictures yet it says so: add some from a game's details. If the screenshot drive is not connected you get the same "Screenshot storage is not available" notice.

![The viewer on the Screenshots page](img/gallery-viewer-phone.jpg)

**Copy them off the Pi** (optional), from a computer on your network, replacing the address:

```sh
rsync -a pi@deltabacklog.local:/mnt/ventodrive/deltabacklog/screenshots/ ./my-screenshots/
```
