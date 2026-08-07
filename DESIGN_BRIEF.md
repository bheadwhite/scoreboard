# Scoreboard — design brief

*A copy-pasteable brief for iterating on the visual design.*

---

I'm building a **family game scoreboard** app and want help designing the look and feel.

## What it is

A mobile web app for families who play games together — cards, board games, ping pong,
whatever. You join a "family," log the games you play, and the app keeps a permanent
record of who beat whom at what.

The entire point is **bragging rights**. Not analytics, not fairness — bragging. The app
exists so my brother-in-law has to look at the number 14–3 and know exactly what it
means. The core stat isn't "your win rate," it's "you have beaten Dad at Rook 14 times
and he has beaten you 3 times." Everything should ladder up to that.

## The one unusual mechanic

Nobody can just declare they won. When a game ends you **claim victory**, and the claim
sits there, publicly pending, until every opponent **concedes**. Only then does it hit
the record. Losers can also **dispute** instead, which freezes it for the family
organizer to sort out. If nobody responds within 24 hours, the concession is assumed.

This is the emotional center of the app. The moment where your phone buzzes and says
"Dad claims he won — do you concede?" should feel like a small, delicious event. It's
half legal ceremony, half schoolyard taunt. I want that screen to have real weight and
a little theater to it.

## Who uses it and where

Adults aged roughly 10 to 75, standing around a kitchen table or sprawled on a couch,
phone in one hand, cards in the other. Often a little rowdy, sometimes a little drunk,
usually not paying full attention. Grandma is on this app.

That means: **mobile-first, one-handed, big tap targets**, readable at arm's length in
bad lighting. The "Concede" button in particular is pressed by someone who is annoyed
and not looking carefully — it needs to be unmistakable and hard to hit by accident.

## Core screens

1. **Family scoreboard** — the landing place. Standings for a family, filterable by
   game. Should feel like a trophy case, not a spreadsheet.
2. **Head-to-head** — you vs. one specific person. The money screen. Big numbers,
   record over time, which games you dominate, which you get crushed at.
3. **Live match** — a game in progress. Who's playing, what the agreed victory
   condition ("first to 500") is, and a big claim-victory button.
4. **The concession prompt** — someone claimed a win, you have to respond. Concede or
   dispute. This is the screen with the drama.
5. **Player profile** — your record, current win streak, and your **nemesis** (the
   person who's beaten you the most).

## Tone

Playful and competitive, but not childish — real adults are using this. Think
handwritten scorekeeping on the back of an envelope, or the chalkboard in a garage
where someone's been tracking ping pong for a decade. Warmth and rivalry at the same
time.

Things I'd love to see explored: a sense of **ceremony** around wins and concessions,
typography that makes numbers feel like *records* rather than data, and some visual
language for streaks, rivalries, and grudges. Trophy/medal iconography is the obvious
move — I'm open to it but interested in something less generic if you have ideas.

Avoid: corporate SaaS dashboard, fintech minimalism, anything that looks like a fitness
tracker. This should have personality.

## Constraints

- React, mobile-first, installable PWA. Works in light and dark.
- Family sizes are small (3–20 people), so no dense data viz needed — it's a handful of
  names and some big numbers.
- Accessible: high contrast, large hit areas, legible for older eyes.

## What I want from you

Explore directions for the overall visual identity — color, typography, the feel of a
"record." Then show me the head-to-head screen and the concession prompt, since those
two carry the whole app.
