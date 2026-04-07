Feature: Shuffle card
  A Shuffle card lets the drawer swap one of their number cards with one of
  an opponent's number cards. Two-stage interaction: first choose a partner
  player, then choose which cards to exchange.

  Background:
    Given Alice has [3, 5]
    And Bob has [7, 9]
    And the deck is [Shuffle]

  Scenario: Shuffle swaps chosen cards between two players
    When Alice draws
    Then a target choice is pending
    When Alice targets Bob
    Then a target choice is pending
    When Alice shuffles 3 for 7
    Then Alice has 7 in her hand
    And Alice does not have 3 in her hand
    And Bob has 3 in his hand
    And Bob does not have 7 in his hand
    And it is Bob's turn

  Scenario: Shuffle is discarded when drawer has no number cards
    Given Alice has []
    And Bob has [7]
    And the deck is [Shuffle]
    When Alice draws
    Then no target choice is pending
    And it is Bob's turn

  Scenario: Shuffle is discarded when no opponent has number cards
    Given Alice has [3]
    And Bob has []
    And the deck is [Shuffle]
    When Alice draws
    Then no target choice is pending
    And it is Bob's turn

  # Note: reaching Flip 7 via Shuffle would require a prior Shuffle to have created
  # a duplicate in the drawer's hand (the only way to give away a card while keeping
  # the unique count at 6, then gain a 7th unique). That multi-round edge case is
  # covered by the triggerFlip7 call in applyShuffleSwap rather than a BDD scenario.
