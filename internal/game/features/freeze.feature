Feature: Freeze card
  Freeze immediately ends the target's round, banking their current score.
  The target is removed from further play this round.
  When only one active player remains, Freeze auto-targets that player.

  Scenario: Drawing Freeze creates a pending target choice
    Given Alice has [5]
    And Bob has [7]
    And the deck is [Freeze]
    When Alice draws
    Then a target choice is pending

  Scenario: Alice freezes Bob
    Given Alice has [5]
    And Bob has [7]
    And the deck is [Freeze]
    When Alice draws
    And Alice targets Bob
    Then Bob is frozen
    And no target choice is pending

  Scenario: Frozen player's score is banked at round end
    Given Alice has [5]
    And Bob has [7]
    And the deck is [Freeze]
    When Alice draws
    And Alice targets Bob
    And Alice stops
    Then Bob's total score is 7

  Scenario: Freeze auto-targets the sole active player — Alice freezes herself
    Given Alice has [5]
    And Bob has [7]
    And Bob is stopped
    And the deck is [Freeze]
    When Alice draws
    Then Alice is frozen
    And no target choice is pending

  Scenario: Alice can freeze herself voluntarily
    Given Alice has [5]
    And Bob has [7]
    And the deck is [Freeze]
    When Alice draws
    And Alice targets herself
    Then Alice is frozen
