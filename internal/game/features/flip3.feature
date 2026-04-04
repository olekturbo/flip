Feature: Flip 3
  Flip 3 forces the target to draw exactly 3 cards from the deck, one at a time.
  The sequence stops early on a bust, a Second Chance save, or a Flip 7.
  Action cards drawn during Flip 3 are deferred and resolved interactively afterwards.

  Scenario: Target draws exactly 3 cards
    Given Alice has [1]
    And Bob has [2]
    And the deck is [Flip 3, 4, 6, 8]
    When Alice draws
    And Alice targets Bob
    Then Bob has 4 in his hand
    And Bob has 6 in his hand
    And Bob has 8 in his hand

  Scenario: Flip 3 stops immediately when target busts
    Given Alice has [1]
    And Bob has [5]
    And the deck is [Flip 3, 3, 5, 9]
    When Alice draws
    And Alice targets Bob
    Then Bob is busted
    And Bob does not have 9 in his hand

  Scenario: Second Chance saves the target mid-Flip 3 and cancels remaining draws
    Given Alice has [1]
    And Bob has [5] and a Second Chance
    And the deck is [Flip 3, 3, 5, 9]
    When Alice draws
    And Alice targets Bob
    Then Bob is not busted
    And Bob no longer has Second Chance
    And Bob does not have 9 in his hand

  Scenario: Flip 7 during Flip 3 ends the round immediately
    Given Alice has [1]
    And Bob has [1, 2, 3, 4, 5, 6]
    And the deck is [Flip 3, 7, 8, 9]
    When Alice draws
    And Alice targets Bob
    Then the round has ended
    And Bob triggered Flip 7

  Scenario: Alice can target herself with Flip 3
    Given Alice has [1]
    And Bob has [2]
    And the deck is [Flip 3, 4, 6, 8]
    When Alice draws
    And Alice targets herself
    Then Alice has 4 in her hand
    And Alice has 6 in her hand
    And Alice has 8 in her hand
